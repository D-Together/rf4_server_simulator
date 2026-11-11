-- 稀有鱼 / 超级稀有鱼「鱼种数」里程碑的计数表：按**钓到过**的鱼种去重。
--
-- 为什么要独立于 catch_log：
--   官方结算界面（801）在玩家选择入护或放生**之前**就把奖座礼物卡画出来了，
--   所以官方的鱼种数一定是在结算（钓到）那一刻计的，不可能等入护。
--   catch_log 只在 14/5 入护时写，用它做分母必然晚一尾，结算界面永远发不出礼物卡。
--
-- 口径：
--   best_grade >= 3 计入「稀有鱼」族（上星或蓝冠），>= 4 计入「超级稀有鱼」族（蓝冠）。
--   同一种鱼反复上星只保留最高档位，鱼种数只算一次（与奖座名「N条稀有鱼」= N 种一致）。
--   只有 grade >= 3 的鱼才落行，普通鱼不进表，表规模约等于「钓到过的稀有种数」。
--
-- 与图鉴的关系：
--   3/17 图鉴、307.f16 星号仍只认 catch_log（入护才收藏），这里只服务奖座。
--   放生的鱼在图鉴里不点亮，但奖座已经按其鱼种数发了 —— 这与官方结算界面
--   先弹卡、玩家随后才决定放生的顺序一致，不是错位。
CREATE TABLE IF NOT EXISTS catch_seen (
    uid        INTEGER NOT NULL,
    fish_key   TEXT    NOT NULL,
    best_grade INTEGER NOT NULL,
    first_ts   REAL    NOT NULL,
    PRIMARY KEY (uid, fish_key)
);

CREATE INDEX IF NOT EXISTS idx_catch_seen_uid_grade ON catch_seen(uid, best_grade);

-- 历史回填：老存档只有 catch_log 一份事实，按鱼种取最高档位补进新表。
-- 放生过的鱼在 catch_log 里没有记录，无法追溯（只影响补丁上线前的进度）。
INSERT OR IGNORE INTO catch_seen(uid, fish_key, best_grade, first_ts)
SELECT uid, fish_key, MAX(grade), MIN(ts)
  FROM catch_log
 WHERE grade >= 3
 GROUP BY uid, fish_key;
