package gameserver

import (
	"context"
	"database/sql"
	"math"
	"time"

	"rf4server/internal/store"
)

const (
	drinkUseReceiptRetention     = 10 * time.Minute
	drinkUseReceiptCleanupPeriod = 5 * time.Minute
)

func (s *Server) drinkRunKey() string {
	s.drinkRunOnce.Do(func() { s.drinkKey = newFishGUID() })
	return s.drinkKey
}

func (s *Server) drinkOnline(uid int64) bool { return s.liveSessionByUID(uid) != nil }

func (s *Server) drinkMaxGap() time.Duration {
	return max(30*time.Second, 3*s.cfg.KeepaliveInterval())
}

func (sess *Session) updateDrinkBonuses(now time.Time) {
	if sess == nil || sess.Srv == nil || sess.Srv.DB == nil || sess.Userid <= 0 {
		return
	}
	s := sess.Srv
	s.drinkMu.Lock()
	defer s.drinkMu.Unlock()
	_, err := s.DB.AdvanceDrinkBonuses(s.dbCtx(), sess.Userid, s.drinkRunKey(), now,
		s.drinkOnline(sess.Userid), s.drinkMaxGap())
	if err != nil {
		s.log.Warn("酒兴剩余时间未保存", "err", err)
	}
}

// drinkXPBonusTx 在首次结算事务内冻结酒兴经验：每瓶按自身剩余时长独立衰减，
// 倍率相加后作为一条 kind 55；回执重放不重新计时或计算。
func (s *Server) drinkXPBonusTx(ctx context.Context, tx *sql.Tx, uid, baseXP int64, now time.Time) (*catchXPBonus, error) {
	states, err := store.AdvanceDrinkBonusesTx(ctx, tx, uid, s.drinkRunKey(), now,
		s.drinkOnline(uid), s.drinkMaxGap())
	if err != nil {
		return nil, err
	}
	var totalRate float64
	var top catchXPBonus
	for _, state := range states {
		remaining := time.Duration(state.RemainingMillis) * time.Millisecond
		rate := drinkRateFor(state.Peak, time.Duration(state.DurationMillis)*time.Millisecond, remaining)
		if state.Peak <= 0 {
			// 兼容旧行：迁移新增的 peak 默认为 0，回退类别默认峰值与时长。
			rate = drinkRate(state.Category, remaining)
		}
		if rate <= 0 {
			continue
		}
		totalRate += rate
		if rate > top.Multiplier {
			guid := state.SourceGUID
			if guid == "" {
				guid = zeroGUID
			}
			top = catchXPBonus{Kind: 55, GUID: guid, Multiplier: rate}
		}
	}
	if totalRate <= 0 {
		return nil, nil
	}
	bonus := catchXPBonus{Kind: 55, GUID: top.GUID, Multiplier: totalRate}
	bonus.XP = int64(math.Round(float64(baseXP) * totalRate))
	if bonus.XP <= 0 {
		return nil, nil
	}
	return &bonus, nil
}

func (s *Server) runDrinkUseReceiptCleanup(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	if s == nil || s.DB == nil {
		return
	}
	s.cleanupDrinkUseReceipts(time.Now())
	ticker := time.NewTicker(drinkUseReceiptCleanupPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			s.cleanupDrinkUseReceipts(now)
		}
	}
}

func (s *Server) cleanupDrinkUseReceipts(now time.Time) {
	if s == nil || s.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	removed, err := s.DB.CleanupDrinkUseReceipts(ctx, now.Add(-drinkUseReceiptRetention))
	if err != nil {
		s.log.Warn("饮酒事务回执清理失败", "err", err)
		return
	}
	if removed > 0 {
		s.log.Info("饮酒事务回执已清理", "rows", removed)
	}
}
