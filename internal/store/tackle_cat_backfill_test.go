package store

import (
	"fmt"
	"testing"
)

// TestCatchLogTackleCatBackfill 覆盖 00055 的历史回填口径。
//
// catch_log 的钓法快照只能来自结算回执：14/4 结算与 14/5 入护共用
// catch-settlement:<uid>:<fishGUID> 回执，回执里的 SkillID 就是当时那次钓获的
// 钓法大类。历史行没有回执时必须保持 0（未知），由 321 侧回退旧口径——
// 不能猜成浮钓，否则会永久污染浮钓纪录卡。
func TestCatchLogTackleCatBackfill(t *testing.T) {
	db := openIntegrityTestDB(t)
	ctx := t.Context()
	const uid = int64(1000001)

	type row struct {
		fish    string
		skillID string // 空表示没有回执
		want    int64
	}
	rows := []row{
		{"coalfish", `{"SkillID":11}`, 11}, // 海钓
		{"a_salmon", `{"SkillID":2}`, 2},   // 路亚
		{"wa_bream", `{"SkillID":1}`, 1},   // 浮钓
		{"carp", `{"SkillID":3}`, 3},       // 底钓
		{"roach", ``, 0},                   // 无回执 → 未知
		{"perch", `{"SkillID":9}`, 0},      // 非法 SkillID → 未知
	}
	for _, r := range rows {
		guid := r.fish + "-guid"
		if _, err := db.SQL.ExecContext(ctx, `INSERT INTO catch_log(
			ts, loc, spot, fish_key, weight_g, length_m, source, bait, grade, xp, silver,
			loc_level, uid, catch_guid, tackle_cat)
			VALUES(1.5, 'loc', 1, ?, 1000, 0.4, 'keep', 'worm', 1, 10, 1, 'level', ?, ?, 0)`,
			r.fish, uid, guid); err != nil {
			t.Fatal(err)
		}
		if r.skillID != "" {
			key := fmt.Sprintf("catch-settlement:%d:%s", uid, guid)
			if _, err := db.SQL.ExecContext(ctx, `INSERT INTO kv(k,v) VALUES(?,?)`, key, r.skillID); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 回执键使用大写 GUID 时也必须能匹配（SQL 侧 lower(catch_guid)）。
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO catch_log(
		ts, loc, spot, fish_key, weight_g, length_m, source, bait, grade, xp, silver,
		loc_level, uid, catch_guid, tackle_cat)
		VALUES(1.5, 'loc', 1, 'upper', 1000, 0.4, 'keep', 'worm', 1, 10, 1, 'level', ?, 'ABCDEF-guid', 0)`,
		uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO kv(k,v) VALUES(?,?)`,
		fmt.Sprintf("catch-settlement:%d:abcdef-guid", uid), `{"SkillID":3}`); err != nil {
		t.Fatal(err)
	}

	countRows := func() (total, filled int) {
		t.Helper()
		if err := db.SQL.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(CASE WHEN tackle_cat<>0 THEN 1 ELSE 0 END),0)
			   FROM catch_log WHERE uid=?`, uid).Scan(&total, &filled); err != nil {
			t.Fatal(err)
		}
		return total, filled
	}
	baselineTotal, _ := countRows()

	// 迁移可能已在 Migrate 阶段执行过；这里显式复算回填语句，验证口径与幂等。
	const backfill = `UPDATE catch_log
	   SET tackle_cat = COALESCE((
	       SELECT CAST(json_extract(kv.v, '$.SkillID') AS INTEGER)
	         FROM kv
	        WHERE kv.k = 'catch-settlement:' || catch_log.uid || ':' || lower(catch_log.catch_guid)
	          AND json_valid(kv.v)
	          AND CAST(json_extract(kv.v, '$.SkillID') AS INTEGER) IN (1, 2, 3, 11)), 0)
	 WHERE tackle_cat = 0 AND catch_guid IS NOT NULL;`
	for i := 0; i < 2; i++ {
		if _, err := db.SQL.ExecContext(ctx, backfill); err != nil {
			t.Fatalf("第 %d 次回填失败: %v", i+1, err)
		}
	}

	// 逐行断言回填值。
	for _, r := range rows {
		var got int64
		if err := db.SQL.QueryRowContext(ctx,
			`SELECT tackle_cat FROM catch_log WHERE uid=? AND fish_key=?`, uid, r.fish).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != r.want {
			t.Fatalf("%s 的 tackle_cat=%d，期望 %d（回执 SkillID=%q）", r.fish, got, r.want, r.skillID)
		}
	}
	var upper int64
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT tackle_cat FROM catch_log WHERE uid=? AND fish_key='upper'`, uid).Scan(&upper); err != nil {
		t.Fatal(err)
	}
	if upper != 3 {
		t.Fatalf("大写 GUID 回执行回填=%d，期望 3", upper)
	}

	// 幂等：两次执行后行数与已填行数都不变，且只落在 {1,2,3,11}。
	// 已填 = 4 条有效回执 + 1 条大写 GUID 行 = 5；roach 无回执、perch 非法 SkillID 保持 0。
	total, filled := countRows()
	if total != baselineTotal || filled != 5 {
		t.Fatalf("回填后行数=%d(基线%d) 已填=%d，期望已填 5", total, baselineTotal, filled)
	}
	var bad int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM catch_log WHERE uid=? AND tackle_cat NOT IN (0,1,2,3,11)`, uid).Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Fatalf("存在 %d 行的 tackle_cat 落在 {1,2,3,11} 之外", bad)
	}
	// 索引存在（幂等创建的 IF NOT EXISTS 索引）。
	var idx int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_catch_log_uid_tackle'`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Fatal("idx_catch_log_uid_tackle 索引缺失")
	}
}

// TestCatchLogTackleCatBackfillBadJSON 覆盖回执 JSON 损坏时不中断迁移：
// json_extract 对非法 JSON 会报错，回填必须靠 IN 过滤把这类行留在 0。
func TestCatchLogTackleCatBackfillBadJSON(t *testing.T) {
	db := openIntegrityTestDB(t)
	ctx := t.Context()
	const uid = int64(1000002)

	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO catch_log(
		ts, loc, spot, fish_key, weight_g, length_m, source, bait, grade, xp, silver,
		loc_level, uid, catch_guid, tackle_cat)
		VALUES(1.5, 'loc', 1, 'broken', 1000, 0.4, 'keep', 'worm', 1, 10, 1, 'level', ?, 'broken-guid', 0)`,
		uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO kv(k,v) VALUES(?,?)`,
		fmt.Sprintf("catch-settlement:%d:broken-guid", uid), `{not-json`); err != nil {
		t.Fatal(err)
	}
	// 损坏 JSON 下不应抛错（该行保持 0），也不能把坏值写进去。
	if _, err := db.SQL.ExecContext(ctx, `UPDATE catch_log
	   SET tackle_cat = COALESCE((
	       SELECT CAST(json_extract(kv.v, '$.SkillID') AS INTEGER)
	         FROM kv
	        WHERE kv.k = 'catch-settlement:' || catch_log.uid || ':' || lower(catch_log.catch_guid)
	          AND json_valid(kv.v)
	          AND CAST(json_extract(kv.v, '$.SkillID') AS INTEGER) IN (1, 2, 3, 11)), 0)
	 WHERE uid=? AND tackle_cat = 0 AND catch_guid IS NOT NULL`, uid); err != nil {
		t.Fatalf("损坏 JSON 导致回填中断: %v", err)
	}
	var got int64
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT tackle_cat FROM catch_log WHERE uid=? AND fish_key='broken'`, uid).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("损坏回执的行应保持 0，实得 %d", got)
	}
}
