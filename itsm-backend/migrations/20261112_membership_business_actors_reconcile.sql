-- 20261112_membership_business_actors_reconcile
-- Phase 4 第二批补漏：事件转问题等非 HTTP 创建路径在代码修复前产生的行没有
-- created_by_membership_id。本迁移幂等重放业务 actors 回填，覆盖发布窗口内新增的 NULL 行。
-- 不修改已有非 NULL 值；读路径仍保留 user_id 兼容。

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
        RAISE NOTICE 'membership business actors reconcile: %.% missing=%',
            r.tbl, r.colname, missing;
    END LOOP;
END $$;
