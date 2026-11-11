package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSkillProficiencyCap(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const uid = int64(1000001)

	if err := db.AddSkillPoints(ctx, uid, 1, 99.9); err != nil {
		t.Fatal(err)
	}
	if err := db.AddSkillPoints(ctx, uid, 1, 0.2); err != nil {
		t.Fatal(err)
	}
	if pts, _ := db.GetSkillPoints(ctx, uid, 1); pts != maxSkillPoints {
		t.Fatalf("熟练度应封顶 %v，实得 %v", maxSkillPoints, pts)
	}
	all, _ := db.ListSkills(ctx, uid)
	if all[1] != maxSkillPoints {
		t.Fatalf("列表熟练度应封顶 %v，实得 %v", maxSkillPoints, all[1])
	}

	if _, err := db.SQL.Exec(`UPDATE skill SET points=110 WHERE uid=? AND skill_id=1`, uid); err != nil {
		t.Fatal(err)
	}
	if pts, _ := db.GetSkillPoints(ctx, uid, 1); pts != maxSkillPoints {
		t.Fatalf("历史熟练度应在读取时封顶，实得 %v", pts)
	}
	if _, err := db.SQL.Exec(`DELETE FROM schema_migrations WHERE name='migrations/00017_skill_points_cap.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	var stored float64
	if err := db.SQL.QueryRow(`SELECT points FROM skill WHERE uid=? AND skill_id=1`, uid).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != maxSkillPoints {
		t.Fatalf("迁移后历史熟练度应封顶 %v，实得 %v", maxSkillPoints, stored)
	}
}

// TestAddSkillPointsQuantizesToDisplayGrid 直接钉住写库 SQL 的 0.1 显示格
// ROUND：AddSkillPoints / AddSkillPointsTx 两处 UPSERT 的初始行与累加分支
// 都必须量化，否则 SQLite REAL 累加 0.1 攒出的尾差（如 99.99999999999996）
// 会再次流入结算，钳位后产生正残渣增量（详见 specs/changes/skill-row-quantize）。
func TestAddSkillPointsQuantizesToDisplayGrid(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const uid = int64(1000001)

	// INSERT 分支：12.34 不是 0.1 格，写库即量化到 12.3。
	if err := db.AddSkillPoints(ctx, uid, 1, 12.34); err != nil {
		t.Fatal(err)
	}
	if pts, _ := db.GetSkillPoints(ctx, uid, 1); pts != 12.3 {
		t.Fatalf("初始行应量化 12.34→12.3，实得 %v", pts)
	}
	// 累加分支：存量尾差 + 0.1 也要落回 0.1 格。
	if _, err := db.SQL.Exec(`UPDATE skill SET points=12.299999999999999 WHERE uid=? AND skill_id=1`, uid); err != nil {
		t.Fatal(err)
	}
	if err := db.AddSkillPoints(ctx, uid, 1, 0.1); err != nil {
		t.Fatal(err)
	}
	if pts, _ := db.GetSkillPoints(ctx, uid, 1); pts != 12.4 {
		t.Fatalf("累加应量化 12.299999999999999+0.1→12.4，实得 %v", pts)
	}

	// AddSkillPointsTx 同款语义：尾差行 + 0.2 → 精确落格。
	if _, err := db.SQL.Exec(`UPDATE skill SET points=12.499999999999998 WHERE uid=? AND skill_id=1`, uid); err != nil {
		t.Fatal(err)
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddSkillPointsTx(ctx, tx, uid, 1, 0.2); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if pts, _ := db.GetSkillPoints(ctx, uid, 1); pts != 12.7 {
		t.Fatalf("事务累加应量化 12.499999999999998+0.2→12.7，实得 %v", pts)
	}
}
