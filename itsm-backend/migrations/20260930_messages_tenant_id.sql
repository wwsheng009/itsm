-- IP-P2-3：messages 租户化（AI 会话消息）
--
-- 背景：messages 原为 conversation 派生表（无 tenant_id），跨租户 RLS/审计需要直接租户列；
-- 决策见 docs/multi-tenant/plan/msp-exempt-tables-quarterly-review.md（2026-09-30 季度复核）。
--
-- 步骤：加列（可空，过渡兼容）→ 从所属会话回填 → 租户维度索引。
-- NOT NULL 收尾：待 scripts/msp/verify-messages-tenant-backfill.sql 巡检归零后单独迁移。

ALTER TABLE messages ADD COLUMN IF NOT EXISTS tenant_id int NULL;

UPDATE messages m
SET tenant_id = c.tenant_id
FROM conversations c
WHERE c.id = m.conversation_id
  AND m.tenant_id IS NULL
  AND c.tenant_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_messages_tenant_conversation ON messages (tenant_id, conversation_id, created_at);
