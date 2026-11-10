package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// 欢乐时光改为跟随会员：永久会员不限时长，限时会员按会员到期时间倒计时，
// 非会员没有资格。happy_hour / happy_hour_exp 列保留但不再作为真值来源；
// happy_hour_state 只用于标记当前在线会话是否正在钓鱼，不再累计每日额度。

// 资格类型历史枚举（会员类型同为 0/1/2，保留常量便于对照）。
const (
	HappyHourNone      = int64(0)
	HappyHourLimited   = int64(1)
	HappyHourUnlimited = int64(2)
)

// HappyHourUnlimitedSeconds 永久会员没有到期时间，主页/档案给一个固定的大额度。
const HappyHourUnlimitedSeconds = int64(9999 * 60)

var happyHourZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type HappyHourEntitlement struct {
	Qualified        bool  `json:"qualified"`
	Unlimited        bool  `json:"unlimited"`
	ExpiresAt        int64 `json:"expires_at"`
	RemainingSeconds int64 `json:"remaining_seconds"`
}

// HappyHourEntitled 按会员类型和到期时间计算欢乐时光资格与剩余时长。
func HappyHourEntitled(kind, expiresAt, now int64) HappyHourEntitlement {
	if kind == HappyHourUnlimited {
		return HappyHourEntitlement{Qualified: true, Unlimited: true, RemainingSeconds: HappyHourUnlimitedSeconds}
	}
	if kind == HappyHourLimited && expiresAt > now {
		return HappyHourEntitlement{Qualified: true, ExpiresAt: expiresAt, RemainingSeconds: expiresAt - now}
	}
	return HappyHourEntitlement{ExpiresAt: expiresAt}
}

type HappyHourStatus struct {
	Enabled          bool    `json:"enabled"`
	Qualified        bool    `json:"qualified"`
	Unlimited        bool    `json:"unlimited"`
	ExpiresAt        int64   `json:"expires_at"`
	Started          bool    `json:"started"`
	Active           bool    `json:"active"`
	RemainingSeconds int64   `json:"remaining_seconds"`
	XPBonus          float64 `json:"xp_bonus"`
	BiteBonus        float64 `json:"bite_bonus"`
	ProficiencyBonus float64 `json:"proficiency_bonus"`
}

func happyHourDay(now time.Time) string { return now.In(happyHourZone).Format("2006-01-02") }

func happyHourStatus(enabled bool, ent HappyHourEntitlement, started, active bool) HappyHourStatus {
	r := HappyHourStatus{Enabled: enabled, Qualified: ent.Qualified, Unlimited: ent.Unlimited, ExpiresAt: ent.ExpiresAt,
		Started: started, Active: enabled && ent.Qualified && active, RemainingSeconds: ent.RemainingSeconds}
	if r.Active {
		r.XPBonus, r.BiteBonus, r.ProficiencyBonus = .5, .1, .25
	}
	return r
}

func happyHourEntitlementTx(ctx context.Context, tx *sql.Tx, uid, now int64) (HappyHourEntitlement, error) {
	var kind, expires int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&kind, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return HappyHourEntitlement{}, nil
	}
	if err != nil {
		return HappyHourEntitlement{}, err
	}
	return HappyHourEntitled(kind, expires, now), nil
}

func (d *DB) happyHourEntitlement(ctx context.Context, uid, now int64) (HappyHourEntitlement, error) {
	var kind, expires int64
	err := d.SQL.QueryRowContext(ctx, `SELECT COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&kind, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return HappyHourEntitlement{}, nil
	}
	if err != nil {
		return HappyHourEntitlement{}, err
	}
	return HappyHourEntitled(kind, expires, now), nil
}

// AdvanceHappyHour 只刷新当前在线会话是否有效：会员资格由会员到期时间决定，
// 不再按天累计/扣除时长，因此在线钓鱼即生效，离线不会消耗任何额度。
func (d *DB) AdvanceHappyHour(ctx context.Context, uid int64, owner string, now time.Time, fishing, takeover bool, maxGap time.Duration) (HappyHourStatus, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return HappyHourStatus{}, err
	}
	defer tx.Rollback()
	// 先取得写锁，多个连接不能同时改写同一账户的在线状态。
	if _, err := tx.ExecContext(ctx, `UPDATE happy_hour_state SET active=active WHERE uid=?`, uid); err != nil {
		return HappyHourStatus{}, err
	}
	enabled := true
	var flag string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM tunables WHERE key='happy_hour_enabled'`).Scan(&flag); err == nil {
		enabled = flag == "1"
	} else if !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	}
	ent, err := happyHourEntitlementTx(ctx, tx, uid, now.Unix())
	if err != nil {
		return HappyHourStatus{}, err
	}
	effective := enabled && ent.Qualified
	var previousOwner string
	var last int64
	var previousActive bool
	err = tx.QueryRowContext(ctx, `SELECT last_tick,session_key,active FROM happy_hour_state WHERE uid=?`, uid).
		Scan(&last, &previousOwner, &previousActive)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	}
	millis := now.UnixMilli()
	// 另一个在线会话仍在活跃时，新会话不能顶掉它，避免两个客户端同时吃加成。
	if exists && previousOwner != owner && (!takeover || previousActive && millis-last < max(30000, maxGap.Milliseconds())) {
		return happyHourStatus(enabled, ent, true, false), nil
	}
	active := fishing && effective
	if !exists && !active {
		return happyHourStatus(enabled, ent, false, false), nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO happy_hour_state(uid,local_day,used_millis,last_tick,session_key,active)
		VALUES(?,?,0,?,?,?) ON CONFLICT(uid) DO UPDATE SET local_day=excluded.local_day,
		used_millis=0,last_tick=excluded.last_tick,session_key=excluded.session_key,active=excluded.active`,
		uid, happyHourDay(now), millis, owner, active); err != nil {
		return HappyHourStatus{}, err
	}
	if err := tx.Commit(); err != nil {
		return HappyHourStatus{}, err
	}
	return happyHourStatus(enabled, ent, true, active), nil
}

func (d *DB) GetHappyHourStatus(ctx context.Context, uid int64, now time.Time) (HappyHourStatus, error) {
	enabled := true
	if flag, found, err := d.TunableGet(ctx, "happy_hour_enabled"); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	} else if found {
		enabled = flag == "1"
	}
	ent, err := d.happyHourEntitlement(ctx, uid, now.Unix())
	if err != nil {
		return HappyHourStatus{}, err
	}
	var last int64
	var active bool
	err = d.SQL.QueryRowContext(ctx, `SELECT active,last_tick FROM happy_hour_state WHERE uid=?`, uid).Scan(&active, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return happyHourStatus(enabled, ent, false, false), nil
	}
	if err != nil {
		return HappyHourStatus{}, err
	}
	return happyHourStatus(enabled, ent, true, active && now.UnixMilli()-last <= 30000), nil
}
