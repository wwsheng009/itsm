-- IP-P0-3 回填脚本：存量工单 MSP 快照（R11 / canon A12）
--
-- 适用：saas_msp 部署升级后，为历史工单补齐 is_managed_by_msp / msp_provider_id。
-- 判据与 service/msp_snapshot.go resolveTicketMSPProvider 对齐：
--   客户租户 active 且未过期；provider 租户存在、类型 msp_provider（兼容 legacy msp）、active。
--
-- 用法（psql）：
--   dry-run（默认，只读）：psql -d <db> -f scripts/msp/backfill-ticket-msp-snapshot.sql
--   apply：               psql -d <db> -v mode=apply -f scripts/msp/backfill-ticket-msp-snapshot.sql
--   rollback：            psql -d <db> -v mode=rollback -f scripts/msp/backfill-ticket-msp-snapshot.sql
--
-- 说明：
--   * dry-run 输出候选计数 + 前 50 行样例；完整清单可复制对应 SELECT 输出到文件留档。
--   * apply 幂等：只触碰 is_managed_by_msp=false 或 msp_provider_id IS NULL 的行。
--   * rollback 限制：缺少批次标记列，回滚按"快照 == 当前客户归属"匹配，会同时清掉
--     运行时正常写入的同 provider 快照；仅用于回填事故回退，执行前先跑 dry-run 留档。
\set ON_ERROR_STOP on

\if :{?mode}
\else
\set mode dry_run
\endif

\if :'mode' = 'dry_run'
SELECT count(*) AS snapshot_candidates
FROM tickets t
JOIN tenants c ON c.id = t.tenant_id
JOIN tenants p ON p.id = c.msp_provider_id
WHERE t.deleted_at IS NULL
  AND c.type IN ('msp_customer', 'customer')
  AND c.status = 'active'
  AND (c.expires_at IS NULL OR c.expires_at > now())
  AND p.type IN ('msp_provider', 'msp')
  AND p.status = 'active'
  AND (t.is_managed_by_msp = false OR t.msp_provider_id IS NULL);

SELECT t.id, t.tenant_id, t.ticket_number, t.is_managed_by_msp, t.msp_provider_id, c.msp_provider_id AS derived_provider_id
FROM tickets t
JOIN tenants c ON c.id = t.tenant_id
JOIN tenants p ON p.id = c.msp_provider_id
WHERE t.deleted_at IS NULL
  AND c.type IN ('msp_customer', 'customer')
  AND c.status = 'active'
  AND (c.expires_at IS NULL OR c.expires_at > now())
  AND p.type IN ('msp_provider', 'msp')
  AND p.status = 'active'
  AND (t.is_managed_by_msp = false OR t.msp_provider_id IS NULL)
ORDER BY t.id
LIMIT 50;

\elif :'mode' = 'apply'
UPDATE tickets t
SET is_managed_by_msp = true,
    msp_provider_id = c.msp_provider_id
FROM tenants c
JOIN tenants p ON p.id = c.msp_provider_id
WHERE t.tenant_id = c.id
  AND t.deleted_at IS NULL
  AND c.type IN ('msp_customer', 'customer')
  AND c.status = 'active'
  AND (c.expires_at IS NULL OR c.expires_at > now())
  AND p.type IN ('msp_provider', 'msp')
  AND p.status = 'active'
  AND (t.is_managed_by_msp = false OR t.msp_provider_id IS NULL);

\echo 'apply done: see UPDATE row count above (backfill-ticket-msp-snapshot)'

\elif :'mode' = 'rollback'
UPDATE tickets t
SET is_managed_by_msp = false,
    msp_provider_id = NULL
FROM tenants c
JOIN tenants p ON p.id = c.msp_provider_id
WHERE t.tenant_id = c.id
  AND t.deleted_at IS NULL
  AND c.type IN ('msp_customer', 'customer')
  AND c.status = 'active'
  AND (c.expires_at IS NULL OR c.expires_at > now())
  AND p.type IN ('msp_provider', 'msp')
  AND p.status = 'active'
  AND t.is_managed_by_msp = true
  AND t.msp_provider_id = c.msp_provider_id;

\echo 'rollback done: snapshot cleared for matching rows (backfill-ticket-msp-snapshot)'

\else
\echo 'invalid mode: use dry_run|apply|rollback'
\endif
