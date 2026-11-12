-- 20261112_service_request_approval_membership
-- C 方案：服务请求审批记录的审批人/转交人补齐租户内 membership 引用。
-- 过渡期保留 approver_id / delegated_to_id；历史无 membership 的审批行保持 NULL。

ALTER TABLE service_request_approvals
    ADD COLUMN IF NOT EXISTS approver_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS delegated_to_membership_id BIGINT;

UPDATE service_request_approvals x
SET approver_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.approver_membership_id IS NULL
  AND x.approver_id IS NOT NULL
  AND m.user_id = x.approver_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

UPDATE service_request_approvals x
SET delegated_to_membership_id = m.id
FROM user_tenant_memberships m
WHERE x.delegated_to_membership_id IS NULL
  AND x.delegated_to_id IS NOT NULL
  AND m.user_id = x.delegated_to_id
  AND m.tenant_id = x.tenant_id
  AND m.deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_service_request_approvals_approver_membership
    ON service_request_approvals (tenant_id, approver_membership_id);
CREATE INDEX IF NOT EXISTS idx_service_request_approvals_delegate_membership
    ON service_request_approvals (tenant_id, delegated_to_membership_id);

-- 清理无法通过租户一致性校验的脏指针，再以 VALID 状态建约束。
UPDATE service_request_approvals x
SET approver_membership_id = NULL
WHERE x.approver_membership_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM user_tenant_memberships m
      WHERE m.id = x.approver_membership_id AND m.tenant_id = x.tenant_id
  );

UPDATE service_request_approvals x
SET delegated_to_membership_id = NULL
WHERE x.delegated_to_membership_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM user_tenant_memberships m
      WHERE m.id = x.delegated_to_membership_id AND m.tenant_id = x.tenant_id
  );

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_service_request_approvals_approver_membership_tenant'
    ) THEN
        ALTER TABLE service_request_approvals
            ADD CONSTRAINT fk_service_request_approvals_approver_membership_tenant
            FOREIGN KEY (approver_membership_id, tenant_id)
            REFERENCES user_tenant_memberships (id, tenant_id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_service_request_approvals_delegate_membership_tenant'
    ) THEN
        ALTER TABLE service_request_approvals
            ADD CONSTRAINT fk_service_request_approvals_delegate_membership_tenant
            FOREIGN KEY (delegated_to_membership_id, tenant_id)
            REFERENCES user_tenant_memberships (id, tenant_id);
    END IF;
END $$;
