-- 鱼饵规则覆盖：管理后台可编辑每个鱼种的吃/不吃/底钓只吃/前置条件，
-- 并可为同一鱼种配置多组饵料定时轮换。没有记录时回落到编译期内置规则表。
CREATE TABLE IF NOT EXISTS fish_bait_override(
    fish_zh     TEXT PRIMARY KEY,
    allow_json  TEXT NOT NULL DEFAULT '[]',
    deny_json   TEXT NOT NULL DEFAULT '[]',
    bottom_json TEXT NOT NULL DEFAULT '[]',
    condition   TEXT NOT NULL DEFAULT '',
    groups_json TEXT NOT NULL DEFAULT '[]',
    rotate_sec  INTEGER NOT NULL DEFAULT 0,
    rotate_mode TEXT NOT NULL DEFAULT 'soft',
    enabled     INTEGER NOT NULL DEFAULT 1,
    updated_by  TEXT NOT NULL DEFAULT '',
    updated_ts  REAL NOT NULL DEFAULT 0
);
