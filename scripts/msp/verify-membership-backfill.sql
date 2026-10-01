-- IP-P1-1 回填巡检（DoD：逐项 0 差异；customer 恰 1 条 active；provider = home + 有效分配）
-- 用法：psql -f scripts/msp/verify-membership-backfill.sql -v ON_ERROR_STOP=1
-- 每节标题下方为"差异行"；无输出 = 该项通过。

\echo '== 0. 回填总览（信息项，非差异） =='
SELECT
  (SELECT count(*) FROM users) AS users_total,
  (SELECT count(*) FROM user_tenant_memberships WHERE source = 'home' AND deleted_at IS NULL) AS home_rows,
  (SELECT count(*) FROM user_tenant_memberships WHERE source = 'allocation' AND deleted_at IS NULL) AS allocation_rows,
  (SELECT count(*) FROM msp_allocations WHERE deassigned_at IS NULL) AS active_allocations;

\echo '== 1. 缺少 home membership 的用户（期望 0 行） =='
SELECT u.id, u.username, u.tenant_id
FROM users u
WHERE u.tenant_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM user_tenant_memberships m
    WHERE m.user_id = u.id AND m.tenant_id = u.tenant_id
      AND m.deleted_at IS NULL AND m.source = 'home'
  );

\echo '== 2. 用户存在多条 active 默认作用域（期望 0 行） =='
SELECT user_id, count(*) AS defaults
FROM user_tenant_memberships
WHERE deleted_at IS NULL AND is_default AND status = 'active'
GROUP BY user_id
HAVING count(*) > 1;

\echo '== 3. customer 账号出现第 2 条 active 作用域（期望 0 行；uq_customer_single_scope 兜底） =='
SELECT user_id, count(*) AS active_scopes
FROM user_tenant_memberships
WHERE deleted_at IS NULL AND status = 'active' AND account_kind = 'customer'
GROUP BY user_id
HAVING count(*) > 1;

\echo '== 4. provider 账号作用域数 != 1(home) + 有效分配数（期望 0 行） =='
WITH prov AS (
  SELECT
    u.id AS user_id,
    u.username,
    t.code AS provider_code,
    (SELECT count(*) FROM msp_allocations a
      WHERE a.msp_user_id = u.id AND a.deassigned_at IS NULL) AS active_allocations,
    (SELECT count(*) FROM user_tenant_memberships m
      WHERE m.user_id = u.id AND m.deleted_at IS NULL AND m.status = 'active') AS live_scopes
  FROM users u
  JOIN tenants t ON t.id = u.tenant_id
  WHERE t.type IN ('msp_provider', 'msp')
)
SELECT *
FROM prov
WHERE live_scopes <> 1 + active_allocations;

\echo '== 5. 有效分配缺少对应 membership（期望 0 行） =='
SELECT a.id AS allocation_id, a.msp_user_id, a.customer_tenant_id, a.role
FROM msp_allocations a
WHERE a.deassigned_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM user_tenant_memberships m
    WHERE m.user_id = a.msp_user_id
      AND m.tenant_id = a.customer_tenant_id
      AND m.deleted_at IS NULL AND m.status = 'active'
  );

\echo '== 6. 服务方作用域 role_id 映射失败（信息项；应随角色 seed 收敛到 0） =='
SELECT m.tenant_id, m.msp_role, count(*) AS rows
FROM user_tenant_memberships m
WHERE m.deleted_at IS NULL
  AND m.msp_role IS NOT NULL
  AND m.role_id IS NULL
GROUP BY m.tenant_id, m.msp_role
ORDER BY m.tenant_id, m.msp_role;

\echo '== 7. 重复存活成员行（期望 0 行；uq_membership_live 兜底） =='
SELECT user_id, tenant_id, count(*) AS live_rows
FROM user_tenant_memberships
WHERE deleted_at IS NULL
GROUP BY user_id, tenant_id
HAVING count(*) > 1;

\echo '== 8. audit_logs.membership_id 未回填余量（信息项） =='
SELECT count(*) AS unfilled
FROM audit_logs al
WHERE al.membership_id IS NULL
  AND al.actor_account IS NOT NULL;
