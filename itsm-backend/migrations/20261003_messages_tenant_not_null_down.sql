-- IP-P2-3 收尾回滚：解除 messages.tenant_id NOT NULL（回到可空过渡态；
-- 数据不回收——已回填的 tenant_id 保留）。
ALTER TABLE messages ALTER COLUMN tenant_id DROP NOT NULL;
