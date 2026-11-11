-- 20261111_membership_tenant_identity
-- C 方案：membership 是租户内身份。
-- 本迁移只做工单垂直切片：补齐缺失 home membership，并回填工单 membership 引用。
-- 过渡期保留 requester_id / assignee_id；membership 字段可空，读路径可回退。

ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS requester_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS assignee_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS created_by_membership_id BIGINT;

-- 1) 为缺少 home membership 的存量账号补一条可追溯的 migration membership。
--    account_kind 按租户类型推导；role_id 按同租户同 code 的角色解析。
INSERT INTO user_tenant_memberships (
    user_id, tenant_id, account_kind, subject_type, source, role_id,
    msp_role, status, is_default, created_at, updated_at
)
SELECT
    u.id,
    u.tenant_id,
    CASE
        WHEN t.type IN ('msp_provider', 'msp') THEN 'provider'
        WHEN t.type IN ('msp_customer', 'saas_customer', 'customer') THEN 'customer'
        ELSE 'platform'
    END,
    'user',
    'migration',
    r.id,
    NULLIF(u.msp_role::text, ''),
    'active',
    true,
    NOW(),
    NOW()
FROM users u
JOIN tenants t ON t.id = u.tenant_id
LEFT JOIN roles r ON r.tenant_id = u.tenant_id AND r.code = u.role::text
WHERE NOT EXISTS (
    SELECT 1
    FROM user_tenant_memberships m
    WHERE m.user_id = u.id
      AND m.tenant_id = u.tenant_id
      AND m.deleted_at IS NULL
)
ON CONFLICT DO NOTHING;

-- 2) 回填工单申请人/处理人的租户内 membership。
UPDATE tickets t
SET requester_membership_id = m.id
FROM user_tenant_memberships m
WHERE t.requester_membership_id IS NULL
  AND m.user_id = t.requester_id
  AND m.tenant_id = t.tenant_id
  AND m.deleted_at IS NULL;

UPDATE tickets t
SET assignee_membership_id = m.id
FROM user_tenant_memberships m
WHERE t.assignee_membership_id IS NULL
  AND t.assignee_id IS NOT NULL
  AND m.user_id = t.assignee_id
  AND m.tenant_id = t.tenant_id
  AND m.deleted_at IS NULL;

-- 存量没有 created_by 字段；先以申请人 membership 作为兼容值。
UPDATE tickets t
SET created_by_membership_id = t.requester_membership_id
WHERE t.created_by_membership_id IS NULL
  AND t.requester_membership_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tickets_requester_membership
    ON tickets (tenant_id, requester_membership_id);
CREATE INDEX IF NOT EXISTS idx_tickets_assignee_membership
    ON tickets (tenant_id, assignee_membership_id);
CREATE INDEX IF NOT EXISTS idx_tickets_created_by_membership
    ON tickets (tenant_id, created_by_membership_id);

-- 3) 复合外键：membership 必须属于工单所在租户。
--    先以 NOT VALID 上线，避免历史脏数据阻塞发布；数据修复后再 VALIDATE。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_tickets_requester_membership_tenant'
    ) THEN
        ALTER TABLE tickets
            ADD CONSTRAINT fk_tickets_requester_membership_tenant
            FOREIGN KEY (requester_membership_id, tenant_id)
            REFERENCES user_tenant_memberships (id, tenant_id)
            NOT VALID;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_tickets_assignee_membership_tenant'
    ) THEN
        ALTER TABLE tickets
            ADD CONSTRAINT fk_tickets_assignee_membership_tenant
            FOREIGN KEY (assignee_membership_id, tenant_id)
            REFERENCES user_tenant_memberships (id, tenant_id)
            NOT VALID;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_tickets_created_by_membership_tenant'
    ) THEN
        ALTER TABLE tickets
            ADD CONSTRAINT fk_tickets_created_by_membership_tenant
            FOREIGN KEY (created_by_membership_id, tenant_id)
            REFERENCES user_tenant_memberships (id, tenant_id)
            NOT VALID;
    END IF;
END $$;
