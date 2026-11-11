-- 20261111_membership_actor_references_down
-- 只回退 Phase 3 的 membership 引用；回填的 membership 关系保留。

DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('ticket_comments', 'fk_ticket_comments_user_membership_tenant'),
            ('ticket_attachments', 'fk_ticket_attachments_uploader_membership_tenant'),
            ('attachments', 'fk_attachments_uploader_membership_tenant'),
            ('ticket_ccs', 'fk_ticket_ccs_user_membership_tenant'),
            ('ticket_ccs', 'fk_ticket_ccs_added_by_membership_tenant'),
            ('ticket_approvals', 'fk_ticket_approvals_approver_membership_tenant'),
            ('ticket_approvals', 'fk_ticket_approvals_delegate_membership_tenant'),
            ('approval_records', 'fk_approval_records_approver_membership_tenant'),
            ('notifications', 'fk_notifications_user_membership_tenant'),
            ('ticket_notifications', 'fk_ticket_notifications_user_membership_tenant'),
            ('process_approval_decisions', 'fk_process_approval_decisions_actor_membership_tenant'),
            ('audit_logs', 'fk_audit_logs_membership_tenant')
        ) AS v(tbl, conname)
    LOOP
        EXECUTE format('ALTER TABLE IF EXISTS %I DROP CONSTRAINT IF EXISTS %I',
            r.tbl, r.conname);
    END LOOP;
END $$;

DROP INDEX IF EXISTS idx_ticket_comments_user_membership;
DROP INDEX IF EXISTS idx_ticket_attachments_uploader_membership;
DROP INDEX IF EXISTS idx_attachments_uploader_membership;
DROP INDEX IF EXISTS idx_ticket_ccs_user_membership;
DROP INDEX IF EXISTS idx_ticket_ccs_added_by_membership;
DROP INDEX IF EXISTS idx_ticket_approvals_approver_membership;
DROP INDEX IF EXISTS idx_ticket_approvals_delegate_membership;
DROP INDEX IF EXISTS idx_approval_records_approver_membership;
DROP INDEX IF EXISTS idx_notifications_user_membership;
DROP INDEX IF EXISTS idx_ticket_notifications_user_membership;
DROP INDEX IF EXISTS idx_process_approval_decisions_actor_membership;
DROP INDEX IF EXISTS idx_audit_logs_membership;

ALTER TABLE ticket_comments DROP COLUMN IF EXISTS user_membership_id;
ALTER TABLE ticket_attachments DROP COLUMN IF EXISTS uploaded_by_membership_id;
ALTER TABLE attachments DROP COLUMN IF EXISTS uploaded_by_membership_id;
ALTER TABLE ticket_ccs
    DROP COLUMN IF EXISTS user_membership_id,
    DROP COLUMN IF EXISTS added_by_membership_id;
ALTER TABLE ticket_approvals
    DROP COLUMN IF EXISTS approver_membership_id,
    DROP COLUMN IF EXISTS delegate_to_membership_id;
ALTER TABLE approval_records DROP COLUMN IF EXISTS approver_membership_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS user_membership_id;
ALTER TABLE ticket_notifications DROP COLUMN IF EXISTS user_membership_id;
ALTER TABLE process_approval_decisions DROP COLUMN IF EXISTS actor_membership_id;
