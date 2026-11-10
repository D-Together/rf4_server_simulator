package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func grantTestHappyHour(t *testing.T, db *DB, uid int64) {
	t.Helper()
	if _, err := db.SQL.Exec(`INSERT INTO profile(uid,name,membership) VALUES(?,'player',?)
		ON CONFLICT(uid) DO UPDATE SET membership=?,membership_exp=0`,
		uid, HappyHourUnlimited, HappyHourUnlimited); err != nil {
		t.Fatal(err)
	}
}

func TestHappyHourFollowsPermanentMembership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "happy.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	grantTestHappyHour(t, db, 101)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, happyHourZone)
	advance := func(owner string, at time.Time, fishing, takeover bool) HappyHourStatus {
		t.Helper()
		r, err := db.AdvanceHappyHour(ctx, 101, owner, at, fishing, takeover, 0)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := advance("a", now, false, true); r.Started || r.Active || !r.Qualified || !r.Unlimited || r.RemainingSeconds != HappyHourUnlimitedSeconds {
		t.Fatalf("idle %+v", r)
	}
	if r := advance("a", now, true, true); !r.Active || r.XPBonus != .5 || r.BiteBonus != .1 || r.ProficiencyBonus != .25 {
		t.Fatalf("start %+v", r)
	}
	if r := advance("a", now.Add(20*time.Second), false, false); r.Active {
		t.Fatalf("pause %+v", r)
	}
	// 前一个会话已暂停时，另一个会话可以接手；不会消耗任何每日额度。
	if r := advance("b", now.Add(time.Hour), true, true); !r.Active {
		t.Fatalf("idle session blocked takeover %+v", r)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if r := advance("c", now.Add(2*time.Hour), true, true); !r.Active || r.RemainingSeconds != HappyHourUnlimitedSeconds {
		t.Fatalf("restart consumed time %+v", r)
	}
	// 连续在线一整天也不会用完（会员无每日时长限制）。
	if r := advance("c", now.Add(24*time.Hour), true, true); !r.Active || !r.Unlimited {
		t.Fatalf("daily cap still enforced %+v", r)
	}
}

func TestHappyHourConcurrentSessionCannotStealActiveSession(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "owners.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx, now := context.Background(), time.Now()
	expires := now.AddDate(0, 0, 30).Unix()
	if _, err := db.SQL.Exec(`INSERT INTO profile(uid,name,membership,membership_exp) VALUES(10,'player',1,?)`, expires); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AdvanceHappyHour(ctx, 10, "first", now, true, true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	other, err := db.AdvanceHappyHour(ctx, 10, "second", now.Add(5*time.Second), true, true, 30*time.Second)
	if err != nil || other.Active {
		t.Fatalf("second session stole active time %+v %v", other, err)
	}
	first, err := db.AdvanceHappyHour(ctx, 10, "first", now.Add(10*time.Second), true, true, 30*time.Second)
	if err != nil || !first.Active || first.RemainingSeconds != expires-now.Unix()-10 {
		t.Fatalf("clock stopped following membership %+v %v", first, err)
	}
}

func TestHappyHourSwitchPausesBonusWithoutTouchingMembership(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "switch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	grantTestHappyHour(t, db, 1)
	now := time.Date(2026, 11, 4, 23, 59, 50, 0, happyHourZone)
	if _, err := db.AdvanceHappyHour(ctx, 1, "a", now, true, true, 0); err != nil {
		t.Fatal(err)
	}
	r, err := db.AdvanceHappyHour(ctx, 1, "a", now.Add(20*time.Second), true, true, 0)
	if err != nil || !r.Active {
		t.Fatalf("member not active %+v %v", r, err)
	}
	if err := db.TunableSet(ctx, "happy_hour_enabled", "0", "test"); err != nil {
		t.Fatal(err)
	}
	r, err = db.AdvanceHappyHour(ctx, 1, "a", now.Add(30*time.Second), true, true, 0)
	if err != nil || r.Active || r.Enabled || !r.Qualified || r.RemainingSeconds != HappyHourUnlimitedSeconds {
		t.Fatalf("disabled %+v %v", r, err)
	}
	if err := db.TunableSet(ctx, "happy_hour_enabled", "1", "test"); err != nil {
		t.Fatal(err)
	}
	r, err = db.AdvanceHappyHour(ctx, 1, "b", now.Add(2*time.Hour), true, true, 0)
	if err != nil || !r.Active {
		t.Fatalf("resumed %+v %v", r, err)
	}
}

func TestHappyHourRequiresValidMembership(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "subscription.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, happyHourZone)
	if r, err := db.AdvanceHappyHour(ctx, 55, "a", now, true, true, 0); err != nil || r.Qualified || r.Active {
		t.Fatalf("非会员仍然生效 %+v %v", r, err)
	}
	expires := now.AddDate(0, 0, 30).Unix()
	if _, err := db.SQL.Exec(`INSERT INTO profile(uid,name,membership,membership_exp) VALUES(55,'p',1,?)`, expires); err != nil {
		t.Fatal(err)
	}
	r, err := db.AdvanceHappyHour(ctx, 55, "a", now, true, true, 0)
	if err != nil || !r.Qualified || r.Unlimited || !r.Active || r.RemainingSeconds != expires-now.Unix() {
		t.Fatalf("限时会员未生效 %+v %v", r, err)
	}
	if r, err := db.AdvanceHappyHour(ctx, 55, "a", now.AddDate(0, 0, 31), true, true, 0); err != nil || r.Qualified || r.Active {
		t.Fatalf("会员过期仍然生效 %+v %v", r, err)
	}
	if err := db.SetMembership(ctx, 55, HappyHourUnlimited, 0, "test", now.Unix()); err != nil {
		t.Fatal(err)
	}
	r, err = db.GetHappyHourStatus(ctx, 55, now)
	if err != nil || !r.Qualified || !r.Unlimited || r.RemainingSeconds != HappyHourUnlimitedSeconds {
		t.Fatalf("永久会员未同步欢乐时光 %+v %v", r, err)
	}
}

func TestHappyHourMigrationPreservesOldProfile(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`DROP TABLE happy_hour_state; DELETE FROM schema_migrations WHERE version=32;
		INSERT INTO profile(uid,name,level,xp,gold) VALUES(7,'旧角色',25,123456,456789)`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Migrate(); err != nil {
			t.Fatal(err)
		}
	}
	p, err := db.GetProfile(context.Background(), 7)
	if err != nil || p.Level != 25 || p.XP != 123456 || p.Gold != 456789 {
		t.Fatalf("profile changed %+v %v", p, err)
	}
	r, err := db.GetHappyHourStatus(context.Background(), 7, time.Now())
	if err != nil || r.Started || r.Active || r.Qualified || r.RemainingSeconds != 0 {
		t.Fatalf("unexpected legacy status %+v %v", r, err)
	}
}

func TestHappyHourConcurrentOwnersShareOneActiveSession(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	grantTestHappyHour(t, db, 18)
	var wg sync.WaitGroup
	var active atomic.Int64
	failures := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := db.AdvanceHappyHour(context.Background(), 18, fmt.Sprintf("owner-%d", i), now, true, true, 30*time.Second)
			if err != nil {
				failures <- err
			} else if r.Active {
				active.Add(1)
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if active.Load() != 1 {
		t.Fatalf("active concurrent owners=%d", active.Load())
	}
	var used int64
	if err := db.SQL.QueryRow(`SELECT used_millis FROM happy_hour_state WHERE uid=18`).Scan(&used); err != nil || used != 0 {
		t.Fatalf("daily budget still recorded=%d %v", used, err)
	}
}

func TestHappyHourMigrationFailureRollsBackAndRetries(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`DROP TABLE happy_hour_state;DELETE FROM schema_migrations WHERE version=32;DELETE FROM tunables WHERE key='happy_hour_enabled';
 INSERT INTO profile(uid,name,level,xp,gold) VALUES(22,'preserved',25,123456,7890);
 CREATE TRIGGER fail_happy_migration BEFORE INSERT ON tunables WHEN NEW.key='happy_hour_enabled' BEGIN SELECT RAISE(ABORT,'test failure');END;`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err == nil {
		t.Fatal("migration unexpectedly succeeded")
	}
	assertCount(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE name='happy_hour_state'`, 0)
	assertCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=32`, 0)
	assertCount(t, db, `SELECT COUNT(*) FROM profile WHERE uid=22 AND level=25 AND xp=123456 AND gold=7890`, 1)
	if _, err := db.SQL.Exec(`DROP TRIGGER fail_happy_migration`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Migrate(); err != nil {
			t.Fatal(err)
		}
	}
	assertCount(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE version=32`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM tunables WHERE key='happy_hour_enabled' AND value='1'`, 1)
}
