-- IP-P2-3 巡检：messages 租户化回填缺口与一致性（只读）
-- 用法：psql "$DATABASE_URL" -f scripts/msp/verify-messages-tenant-backfill.sql
-- 归零判据：query1 = 0 AND query2 = 0，且 query3 的会话无租户范围已明确处置（否则 NOT NULL 收尾阻塞）。

-- 1) 未回填（tenant_id 为空）但所属会话有租户 → 继续执行回填
SELECT count(*) AS unfilled_with_conversation_tenant
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE m.tenant_id IS NULL
  AND c.tenant_id IS NOT NULL;

-- 2) 与所属会话租户不一致（应为 0；非 0 = 越权写入或回填错误，需人工核）
SELECT count(*) AS mismatched_rows
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE m.tenant_id IS DISTINCT FROM c.tenant_id;

-- 3) 会话自身无租户的历史范围（NOT NULL 收尾前需明确：补租户 / 清理 / 单独豁免）
SELECT count(*) AS messages_of_tenantless_conversations
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
WHERE c.tenant_id IS NULL;
