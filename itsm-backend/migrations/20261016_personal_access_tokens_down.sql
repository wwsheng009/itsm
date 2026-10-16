-- 20261016 个人访问令牌（PAT）回滚：仅删除本批新增的索引与归因列。
-- 注意（实施文档 §5）：down.sql 仅在同时回滚 ent schema 变更时语义成立——
-- ent/schema/auditlog.go 的 token_id 字段未回滚时，下次启动 Schema.Create 会把该列重新加回
-- （internal/bootstrap/app.go 顺序为 ent 先建、SQL 后补）。
-- 已产生的令牌行保留，避免误删审计线索。
DROP INDEX IF EXISTS idx_audit_source_created;
DROP INDEX IF EXISTS idx_audit_token;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS token_id;
