-- IP-P2-3 down：messages 去租户化（回滚）
DROP INDEX IF EXISTS idx_messages_tenant_conversation;
ALTER TABLE messages DROP COLUMN IF EXISTS tenant_id;
