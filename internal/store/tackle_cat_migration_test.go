package store

import (
	"path/filepath"
	"testing"
)

// TestCatchTackleCatMigrationWhenColumnExists 覆盖分支库升级场景。
//
// 分支库可能用早期编号建过 tackle_cat 列，并在 schema_migrations 留下旧登记行；
// 该编号后来被上游另一条迁移占用。主线新编号的迁移再跑时必须：
//  1. 不因重复 ADD COLUMN 失败；
//  2. 让旧登记行退役，把编号让给上游迁移；
//  3. 正常登记自己为已应用。
func TestCatchTackleCatMigrationWhenColumnExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "branch.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("首次迁移: %v", err)
	}

	// 还原成分支库形态：删掉新登记，写入"旧编号 + 本迁移文件名"的登记行。
	// 编号 54 已被上游迁移占用，这里用退役段编号模拟它的历史位置——
	// retireStaleMigrationRows 只关心"名字不在目录里"，不关心编号落点。
	if _, err := db.SQL.Exec(`DELETE FROM schema_migrations WHERE name=?`,
		"migrations/00055_catch_tackle_cat.sql"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)`,
		9_000_054, "migrations/00054_catch_tackle_cat.sql", 1.0); err != nil {
		t.Fatalf("写入旧登记行: %v", err)
	}

	// 列已存在的前提下再次迁移：必须成功，且只做回填。
	if err := db.Migrate(); err != nil {
		t.Fatalf("列已存在时二次迁移失败: %v", err)
	}

	var exists int
	if err := db.SQL.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('catch_log') WHERE name='tackle_cat'`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 1 {
		t.Fatal("tackle_cat 列应存在")
	}
	var applied int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name=?`,
		"migrations/00055_catch_tackle_cat.sql").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatal("00055 应登记为已应用")
	}
}
