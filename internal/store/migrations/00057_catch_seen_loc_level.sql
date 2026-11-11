-- catch_seen 增加「在哪个水域钓到的」这一维：水域奖杯也要在结算界面弹卡。
--
-- 为什么要改：
--   稀有鱼奖座（来源码 5）与水域奖杯（来源码 6）在官服都是**结算界面当场弹卡**。
--   奖座已经按「钓到过」的鱼种数判定（catch_seen），但水域奖杯要判断的是
--   「这个水域的面板是否集齐」，必须知道每一尾鱼是在哪个水域钓到的。
--   catch_log 只记入护，拿不到「结算时点」的事实；catch_seen 记钓到，缺水域维度。
--   所以给 catch_seen 补 loc_level，按 (uid, loc_level, fish_key) 去重：
--   同一种鱼在不同水域各算一条，稀有鱼「鱼种数」仍用 COUNT(DISTINCT fish_key)
--   跨水域去重，口径不变。
--
-- 回填：
--   老存档只有 catch_log 一份带位置的事实，先按 (uid, loc_level, fish_key) 回填；
--   再把旧 catch_seen 里剩下的（放生过的、catch_log 没有的）按 loc_level='' 补进来，
--   这部分只服务稀有鱼计数，不参与任何水域判定（水域判定要求 loc_level 精确匹配）。
CREATE TABLE catch_seen_new (
    uid        INTEGER NOT NULL,
    loc_level  TEXT    NOT NULL DEFAULT '',
    fish_key   TEXT    NOT NULL,
    best_grade INTEGER NOT NULL,
    first_ts   REAL    NOT NULL,
    PRIMARY KEY (uid, loc_level, fish_key)
);

INSERT OR IGNORE INTO catch_seen_new(uid, loc_level, fish_key, best_grade, first_ts)
SELECT uid, COALESCE(loc_level,''), fish_key, MAX(grade), MIN(ts)
  FROM catch_log
 WHERE grade >= 3
 GROUP BY uid, COALESCE(loc_level,''), fish_key;

INSERT OR IGNORE INTO catch_seen_new(uid, loc_level, fish_key, best_grade, first_ts)
SELECT uid, '', fish_key, best_grade, first_ts FROM catch_seen;

DROP TABLE catch_seen;
ALTER TABLE catch_seen_new RENAME TO catch_seen;

CREATE INDEX IF NOT EXISTS idx_catch_seen_uid_grade ON catch_seen(uid, best_grade);
CREATE INDEX IF NOT EXISTS idx_catch_seen_uid_loc ON catch_seen(uid, loc_level, best_grade);
