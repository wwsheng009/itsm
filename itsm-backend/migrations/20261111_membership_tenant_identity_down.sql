-- 20261111_membership_tenant_identity_down
-- 只回退工单 membership 引用；source=migration 的 home membership 属于数据修复，
-- 不在此处删除，避免把真实身份关系一并回滚。

ALTER TABLE tickets DROP CONSTRAINT IF EXISTS fk_tickets_requester_membership_tenant;
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS fk_tickets_assignee_membership_tenant;
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS fk_tickets_created_by_membership_tenant;

DROP INDEX IF EXISTS idx_tickets_requester_membership;
DROP INDEX IF EXISTS idx_tickets_assignee_membership;
DROP INDEX IF EXISTS idx_tickets_created_by_membership;

ALTER TABLE tickets
    DROP COLUMN IF EXISTS requester_membership_id,
    DROP COLUMN IF EXISTS assignee_membership_id,
    DROP COLUMN IF EXISTS created_by_membership_id;
