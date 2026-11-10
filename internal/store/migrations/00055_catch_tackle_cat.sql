-- 统计页钓法族归属：catch_log 增加钓法大类快照。
--
-- 取值与技能大类同源（skills_ops.go fishingSkillCatForRig / 客户端 SkillType）：
--   1=浮钓 float  2=路亚/纺车 spinning  3=底钓 feeder  11=海钓 marine  0=未知
--
-- 此前 321 统计只能按「饵是不是拟饵」二选一，海钓渔获（如挪威海绿青鳕）被算进
-- 浮钓纪录。落库快照后 f14/f15/f16 与竿/钓组/饵料聚合才能按真实钓法分族。
--
-- 列可能已由早期分支编号建过，这里按列是否存在决定是否新增，保证重复执行安全。
-- （SQLite 的 ALTER TABLE ADD COLUMN 不支持 IF NOT EXISTS。）
ALTER TABLE catch_log ADD COLUMN tackle_cat INTEGER NOT NULL DEFAULT 0;

-- 历史回填：只用已经存在的结算回执真源，不猜。
-- 14/4 结算与 14/5 入护共用 catch-settlement:<uid>:<fishGUID> 回执，回执里的
-- SkillID 就是当时那次钓获的钓法大类；catch_log.catch_guid 即该 fishGUID。
--
-- 用 json_valid() 先过滤：JSON 损坏的回执若直接交给 json_extract 会抛
-- "malformed JSON" 并中断整条迁移（一个坏回执就让服务起不来）。损坏行跳过，
-- 保持 0（未知），由 321 侧回退旧口径。
-- 没有回执的历史行同样保持 0。
UPDATE catch_log
   SET tackle_cat = COALESCE((
       SELECT CAST(json_extract(kv.v, '$.SkillID') AS INTEGER)
         FROM kv
        WHERE kv.k = 'catch-settlement:' || catch_log.uid || ':' || lower(catch_log.catch_guid)
          AND json_valid(kv.v)
          AND CAST(json_extract(kv.v, '$.SkillID') AS INTEGER) IN (1, 2, 3, 11)), 0)
 WHERE tackle_cat = 0 AND catch_guid IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_catch_log_uid_tackle ON catch_log(uid, tackle_cat);
