-- 项目管理 P1 阶段门（PE-12）回滚
--
-- 说明：仅回滚索引兜底；表由 ent 管理（如需彻底回滚：
--      DROP TABLE IF EXISTS project_gates; —— 会丢失阶段门历史，谨慎使用）。

DROP INDEX IF EXISTS uk_project_gates;
DROP INDEX IF EXISTS project_gates_project_id_status;
