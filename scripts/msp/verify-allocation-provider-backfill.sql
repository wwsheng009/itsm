-- IP-P2-1 巡检：msp_allocations.provider_tenant_id 回填与一致性（只读）
-- 用法：psql "$DSN" -f scripts/msp/verify-allocation-provider-backfill.sql
-- 期望：三个查询全部返回 0 行/0 计数；NOT NULL 收尾前以此为准。

\echo '1) 未回填（活跃分配 provider IS NULL）：'
SELECT count(*) AS null_active
FROM msp_allocations
WHERE deassigned_at IS NULL AND provider_tenant_id IS NULL;

\echo '2) 与客户归属不一致（provider != customer.msp_provider_id）：'
SELECT a.id, a.msp_user_id, a.customer_tenant_id, a.provider_tenant_id, c.msp_provider_id AS customer_provider
FROM msp_allocations a
JOIN tenants c ON c.id = a.customer_tenant_id
WHERE a.provider_tenant_id IS DISTINCT FROM c.msp_provider_id;

\echo '3) provider 指向非 msp_provider 租户：'
SELECT a.id, t.id AS provider_id, t.type
FROM msp_allocations a
JOIN tenants t ON t.id = a.provider_tenant_id
WHERE t.type <> 'msp_provider';

\echo '4) 完整差异清单（含已解除分配，供人工复核）：'
SELECT a.id, a.msp_user_id, a.customer_tenant_id, a.provider_tenant_id, c.msp_provider_id AS customer_provider, a.deassigned_at
FROM msp_allocations a
JOIN tenants c ON c.id = a.customer_tenant_id
WHERE a.provider_tenant_id IS DISTINCT FROM c.msp_provider_id
ORDER BY a.id;
