-- 20261112_membership_business_actors
-- C 方案 Phase 4 第二批 A：问题 / 变更 / 服务请求的创建人与处理人
-- 从 user_id 单轨补齐为 membership 引用。过渡期保留 user_id，membership 可空；
-- 新写入在可解析时填充，读路径回退 user_id。

-- 1) 为仍缺少 home membership 的存量账号补一条可追溯的 migration membership。
--    与 20261111_membership_tenant_identity 同口径，幂等执行，确保回填有目标行。
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

-- 2) 加列。
ALTER TABLE problems
    ADD COLUMN IF NOT EXISTS created_by_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS assignee_membership_id BIGINT;
ALTER TABLE changes
    ADD COLUMN IF NOT EXISTS created_by_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS assignee_membership_id BIGINT;
ALTER TABLE service_requests
    ADD COLUMN IF NOT EXISTS requester_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS processor_membership_id BIGINT;

-- 3) 回填历史行：只认同租户、未软删的 membership；无匹配保持 NULL 进入巡检。
UPDATE problems p
SET created_by_membership_id = m.id
FROM user_tenant_memberships m
WHERE p.created_by_membership_id IS NULL
  AND m.user_id = p.created_by
  AND m.tenant_id = p.tenant_id
  AND m.deleted_at IS NULL;

UPDATE problems p
SET assignee_membership_id = m.id
FROM user_tenant_memberships m
WHERE p.assignee_membership_id IS NULL
  AND p.assignee_id IS NOT NULL
  AND p.assignee_id > 0
  AND m.user_id = p.assignee_id
  AND m.tenant_id = p.tenant_id
  AND m.deleted_at IS NULL;

UPDATE changes c
SET created_by_membership_id = m.id
FROM user_tenant_memberships m
WHERE c.created_by_membership_id IS NULL
  AND m.user_id = c.created_by
  AND m.tenant_id = c.tenant_id
  AND m.deleted_at IS NULL;

UPDATE changes c
SET assignee_membership_id = m.id
FROM user_tenant_memberships m
WHERE c.assignee_membership_id IS NULL
  AND c.assignee_id IS NOT NULL
  AND c.assignee_id > 0
  AND m.user_id = c.assignee_id
  AND m.tenant_id = c.tenant_id
  AND m.deleted_at IS NULL;

UPDATE service_requests sr
SET requester_membership_id = m.id
FROM user_tenant_memberships m
WHERE sr.requester_membership_id IS NULL
  AND m.user_id = sr.requester_id
  AND m.tenant_id = sr.tenant_id
  AND m.deleted_at IS NULL;

UPDATE service_requests sr
SET processor_membership_id = m.id
FROM user_tenant_memberships m
WHERE sr.processor_membership_id IS NULL
  AND sr.processor_id IS NOT NULL
  AND sr.processor_id > 0
  AND m.user_id = sr.processor_id
  AND m.tenant_id = sr.tenant_id
  AND m.deleted_at IS NULL;

-- 4) 租户作用域索引。
CREATE INDEX IF NOT EXISTS idx_problems_created_by_membership
    ON problems (tenant_id, created_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_problems_assignee_membership
    ON problems (tenant_id, assignee_membership_id);
CREATE INDEX IF NOT EXISTS idx_changes_created_by_membership
    ON changes (tenant_id, created_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_changes_assignee_membership
    ON changes (tenant_id, assignee_membership_id);
CREATE INDEX IF NOT EXISTS idx_service_requests_requester_membership
    ON service_requests (tenant_id, requester_membership_id);
CREATE INDEX IF NOT EXISTS idx_service_requests_processor_membership
    ON service_requests (tenant_id, processor_membership_id);

-- 5) 复合外键：membership 必须属于该行 tenant_id。
--    先 NOT VALID 上线，避免历史无成员数据阻塞发布；
--    同批 20261112_membership_constraints_validate 负责清理脏引用后 VALIDATE。
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('problems', 'fk_problems_created_by_membership_tenant', 'created_by_membership_id'),
            ('problems', 'fk_problems_assignee_membership_tenant', 'assignee_membership_id'),
            ('changes', 'fk_changes_created_by_membership_tenant', 'created_by_membership_id'),
            ('changes', 'fk_changes_assignee_membership_tenant', 'assignee_membership_id'),
            ('service_requests', 'fk_service_requests_requester_membership_tenant', 'requester_membership_id'),
            ('service_requests', 'fk_service_requests_processor_membership_tenant', 'processor_membership_id')
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

DO $$
DECLARE
    r record;
    missing bigint;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('problems', 'created_by_membership_id'),
            ('problems', 'assignee_membership_id'),
            ('changes', 'created_by_membership_id'),
            ('changes', 'assignee_membership_id'),
            ('service_requests', 'requester_membership_id'),
            ('service_requests', 'processor_membership_id')
        ) AS v(tbl, colname)
    LOOP
        EXECUTE format('SELECT count(*) FROM %I WHERE %I IS NULL', r.tbl, r.colname)
            INTO missing;
        RAISE NOTICE 'membership business actors backfill: %.% missing=%',
            r.tbl, r.colname, missing;
    END LOOP;
END $$;
