-- IP-P2-6 回滚：移除租户硬配额列（超限校验随代码回滚自然失效）。
ALTER TABLE tenants DROP COLUMN IF EXISTS quota;
