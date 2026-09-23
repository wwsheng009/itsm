-- 20260922 attachments 表回滚（对应 20260922_create_attachments_expand.sql）。
-- 仅用于 expand 阶段回滚：P2 回填/双写之后回滚需先确认新链路无数据（关开关即可，见方案 §6.1）。

BEGIN;

DROP POLICY IF EXISTS tenant_isolation_attachments ON attachments;
ALTER TABLE attachments DISABLE ROW LEVEL SECURITY;
DROP TABLE IF EXISTS attachments;

COMMIT;
