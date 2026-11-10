-- 同类酒可叠加：每瓶一条效果，各自计时；同一时刻最多 10 瓶生效。
-- 旧表以 (uid,category) 为主键，只能保存每类一条；重建为自增 id 逐瓶表并迁移旧行。
ALTER TABLE drink_bonus_effect RENAME TO drink_bonus_effect_legacy_00054;

CREATE TABLE drink_bonus_effect(
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    uid              INTEGER NOT NULL,
    category         TEXT    NOT NULL CHECK(length(category)>0),
    peak             REAL    NOT NULL DEFAULT 0 CHECK(peak>=0 AND peak<=1),
    remaining_millis INTEGER NOT NULL CHECK(remaining_millis>=0),
    duration_millis  INTEGER NOT NULL CHECK(duration_millis>0),
    source_guid      TEXT    NOT NULL DEFAULT '',
    CHECK(remaining_millis<=duration_millis)
);

INSERT INTO drink_bonus_effect(uid,category,peak,remaining_millis,duration_millis,source_guid)
SELECT uid,category,peak,remaining_millis,duration_millis,source_guid
FROM drink_bonus_effect_legacy_00054;

DROP TABLE drink_bonus_effect_legacy_00054;

CREATE INDEX IF NOT EXISTS idx_drink_bonus_effect_uid ON drink_bonus_effect(uid);
