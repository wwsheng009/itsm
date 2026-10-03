-- IP-P2-6 平台租户管理：租户硬配额（limits）模型落库。
-- 语义：tenants.quota 为 jsonb；NULL / 空对象 / 值<=0 = 不限（fail-open，行为不变）。
-- 允许键（写入按显式模型校验，未知键拒绝）：maxUsers / maxTicketsPerMonth / maxStorageMB(MB)。
-- 幂等：ADD COLUMN IF NOT EXISTS；存量行保持 NULL（不限）。

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS quota jsonb;

COMMENT ON COLUMN tenants.quota IS '租户硬配额（limits）：{maxUsers,maxTicketsPerMonth,maxStorageMB}；NULL 或值<=0 = 不限（IP-P2-6）';
