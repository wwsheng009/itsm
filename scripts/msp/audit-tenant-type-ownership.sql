-- 存量租户类型/归属巡检（IP-P0-4；canon A1/A2、D2/R3）
--
-- 用法（只读）：psql -d <db> -f scripts/msp/audit-tenant-type-ownership.sql
-- 判据与 pkg/tenantmode（ValidateTenantTypeForWrite / ValidateTenantOwnership）对齐。
-- 输出各违规类别明细 + 汇总计数；非零计数 = 需要 P1 数据收敛（本脚本不改数据）。

\set ON_ERROR_STOP on

\echo '== 1. legacy/未知类型（写入已拒绝，存量需 P1 收敛）=='
SELECT id, code, name, type, status
FROM tenants
WHERE type NOT IN ('internal', 'msp_provider', 'msp_customer', 'saas_customer')
ORDER BY id;

\echo '== 2. 归属形状违规（msp_customer 缺 provider / 非客户类型带 provider）=='
SELECT id, code, name, type, coalesce(msp_provider_id, 0) AS provider_id
FROM tenants
WHERE (type = 'msp_customer' AND coalesce(msp_provider_id, 0) = 0)
   OR (type IN ('saas_customer', 'internal', 'msp_provider') AND coalesce(msp_provider_id, 0) > 0)
ORDER BY id;

\echo '== 3. 归属目标无效（不存在 / 非 provider 类型 / 自指）=='
SELECT c.id, c.code, c.name, c.type, c.msp_provider_id,
       p.id AS provider_row_id, p.type AS provider_type
FROM tenants c
LEFT JOIN tenants p ON p.id = c.msp_provider_id
WHERE c.type = 'msp_customer'
  AND c.msp_provider_id IS NOT NULL
  AND (p.id IS NULL OR p.type NOT IN ('msp_provider', 'msp') OR p.id = c.id)
ORDER BY c.id;

\echo '== 4. parent_tenant_id 双写残留（P1 回填参考：与 msp_provider_id 不一致或仅有 legacy 值）=='
SELECT id, code, type, coalesce(parent_tenant_id, 0) AS parent_id, coalesce(msp_provider_id, 0) AS provider_id
FROM tenants
WHERE coalesce(parent_tenant_id, 0) > 0
  AND coalesce(parent_tenant_id, 0) <> coalesce(msp_provider_id, 0)
ORDER BY id;

\echo '== 5. 汇总计数（全部应为 0 才满足 A1/A2 存量巡检）=='
SELECT 'legacy_or_unknown_type' AS violation, count(*) AS rows
FROM tenants
WHERE type NOT IN ('internal', 'msp_provider', 'msp_customer', 'saas_customer')
UNION ALL
SELECT 'ownership_shape', count(*)
FROM tenants
WHERE (type = 'msp_customer' AND coalesce(msp_provider_id, 0) = 0)
   OR (type IN ('saas_customer', 'internal', 'msp_provider') AND coalesce(msp_provider_id, 0) > 0)
UNION ALL
SELECT 'provider_target_invalid', count(*)
FROM tenants c
LEFT JOIN tenants p ON p.id = c.msp_provider_id
WHERE c.type = 'msp_customer'
  AND c.msp_provider_id IS NOT NULL
  AND (p.id IS NULL OR p.type NOT IN ('msp_provider', 'msp') OR p.id = c.id)
ORDER BY violation;
