-- 20261111_membership_actor_references
-- C 方案 Phase 3：把工单评论/附件/CC/审批与通知/审计/BPMN 决策的 actor/recipient
-- 从 user_id 单轨补齐为 membership 引用。过渡期保留 user_id，membership 可空；
-- 新写入在可解析时填充，读路径回退 user_id。

ALTER TABLE ticket_comments
    ADD COLUMN IF NOT EXISTS user_membership_id BIGINT;
ALTER TABLE ticket_attachments
    ADD COLUMN IF NOT EXISTS uploaded_by_membership_id BIGINT;
ALTER TABLE attachments
    ADD COLUMN IF NOT EXISTS uploaded_by_membership_id BIGINT;
ALTER TABLE ticket_ccs
    ADD COLUMN IF NOT EXISTS user_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS added_by_membership_id BIGINT;
ALTER TABLE ticket_approvals
    ADD COLUMN IF NOT EXISTS approver_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS delegate_to_membership_id BIGINT;
ALTER TABLE approval_records
    ADD COLUMN IF NOT EXISTS approver_membership_id BIGINT;
ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS user_membership_id BIGINT;
ALTER TABLE ticket_notifications
    ADD COLUMN IF NOT EXISTS user_membership_id BIGINT;
ALTER TABLE process_approval_decisions
    ADD COLUMN IF NOT EXISTS actor_membership_id BIGINT;

-- 1) 回填历史行：只认同租户、未软删的 membership；无匹配保持 NULL 进入巡检。
UPDATE ticket_comments x
SET user_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.user_membership_id IS NULL
  AND m.user_id = x.user_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE ticket_attachments x
SET uploaded_by_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.uploaded_by_membership_id IS NULL
  AND m.user_id = x.uploaded_by
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE attachments x
SET uploaded_by_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.uploaded_by_membership_id IS NULL
  AND m.user_id = x.uploaded_by
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE ticket_ccs x
SET user_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.user_membership_id IS NULL
  AND m.user_id = x.user_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE ticket_ccs x
SET added_by_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.added_by_membership_id IS NULL
  AND m.user_id = x.added_by
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE ticket_approvals x
SET approver_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.approver_membership_id IS NULL
  AND m.user_id = x.approver_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE ticket_approvals x
SET delegate_to_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.delegate_to_membership_id IS NULL
  AND x.delegate_to_user_id > 0
  AND m.user_id = x.delegate_to_user_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE approval_records x
SET approver_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.approver_membership_id IS NULL
  AND m.user_id = x.approver_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE notifications x
SET user_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.user_membership_id IS NULL
  AND m.user_id = x.user_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE ticket_notifications x
SET user_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.user_membership_id IS NULL
  AND m.user_id = x.user_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE process_approval_decisions x
SET actor_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.actor_membership_id IS NULL
  AND x.actor_id > 0
  AND m.user_id = x.actor_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

-- 2) 按租户作用域建立索引。
CREATE INDEX IF NOT EXISTS idx_ticket_comments_user_membership
    ON ticket_comments (tenant_id, user_membership_id);
CREATE INDEX IF NOT EXISTS idx_ticket_attachments_uploader_membership
    ON ticket_attachments (tenant_id, uploaded_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_attachments_uploader_membership
    ON attachments (tenant_id, uploaded_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_ticket_ccs_user_membership
    ON ticket_ccs (tenant_id, user_membership_id);
CREATE INDEX IF NOT EXISTS idx_ticket_ccs_added_by_membership
    ON ticket_ccs (tenant_id, added_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_ticket_approvals_approver_membership
    ON ticket_approvals (tenant_id, approver_membership_id);
CREATE INDEX IF NOT EXISTS idx_ticket_approvals_delegate_membership
    ON ticket_approvals (tenant_id, delegate_to_membership_id);
CREATE INDEX IF NOT EXISTS idx_approval_records_approver_membership
    ON approval_records (tenant_id, approver_membership_id);
CREATE INDEX IF NOT EXISTS idx_notifications_user_membership
    ON notifications (tenant_id, user_membership_id);
CREATE INDEX IF NOT EXISTS idx_ticket_notifications_user_membership
    ON ticket_notifications (tenant_id, user_membership_id);
CREATE INDEX IF NOT EXISTS idx_process_approval_decisions_actor_membership
    ON process_approval_decisions (tenant_id, actor_membership_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_membership
    ON audit_logs (tenant_id, membership_id);

-- 3) 复合外键：membership 必须属于该行 tenant_id。
--    与 tickets 一致先 NOT VALID 上线，避免历史无成员数据阻塞发布；数据修复后 VALIDATE。
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('ticket_comments', 'fk_ticket_comments_user_membership_tenant', 'user_membership_id'),
            ('ticket_attachments', 'fk_ticket_attachments_uploader_membership_tenant', 'uploaded_by_membership_id'),
            ('attachments', 'fk_attachments_uploader_membership_tenant', 'uploaded_by_membership_id'),
            ('ticket_ccs', 'fk_ticket_ccs_user_membership_tenant', 'user_membership_id'),
            ('ticket_ccs', 'fk_ticket_ccs_added_by_membership_tenant', 'added_by_membership_id'),
            ('ticket_approvals', 'fk_ticket_approvals_approver_membership_tenant', 'approver_membership_id'),
            ('ticket_approvals', 'fk_ticket_approvals_delegate_membership_tenant', 'delegate_to_membership_id'),
            ('approval_records', 'fk_approval_records_approver_membership_tenant', 'approver_membership_id'),
            ('notifications', 'fk_notifications_user_membership_tenant', 'user_membership_id'),
            ('ticket_notifications', 'fk_ticket_notifications_user_membership_tenant', 'user_membership_id'),
            ('process_approval_decisions', 'fk_process_approval_decisions_actor_membership_tenant', 'actor_membership_id'),
            ('audit_logs', 'fk_audit_logs_membership_tenant', 'membership_id')
        ) AS v(tbl, conname, colname)
    LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_constraint WHERE conname = r.conname
        ) THEN
            EXECUTE format(
                'ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY (%I, tenant_id) REFERENCES user_tenant_memberships (id, tenant_id) NOT VALID',
                r.tbl, r.conname, r.colname
            );
        END IF;
    END LOOP;
END $$;
