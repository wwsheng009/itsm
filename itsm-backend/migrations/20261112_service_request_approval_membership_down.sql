-- 20261112_service_request_approval_membership (down)
-- 回滚服务请求审批记录的 membership 引用。

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_service_request_approvals_approver_membership_tenant'
    ) THEN
        ALTER TABLE service_request_approvals
            DROP CONSTRAINT fk_service_request_approvals_approver_membership_tenant;
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_service_request_approvals_delegate_membership_tenant'
    ) THEN
        ALTER TABLE service_request_approvals
            DROP CONSTRAINT fk_service_request_approvals_delegate_membership_tenant;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_service_request_approvals_approver_membership;
DROP INDEX IF EXISTS idx_service_request_approvals_delegate_membership;

ALTER TABLE service_request_approvals
    DROP COLUMN IF EXISTS approver_membership_id,
    DROP COLUMN IF EXISTS delegated_to_membership_id;
