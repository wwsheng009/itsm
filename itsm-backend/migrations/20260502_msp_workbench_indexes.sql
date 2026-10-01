-- IP-P0-7 跨客户工作台索引（工作台方案 §5；先查缺再建，幂等）
-- 说明：Ent schema 已有 tickets(tenant_id,status) 索引；工作台按 (tenant_id,status,updated_at)
-- 与 (tenant_id,assignee_id,status)、(tenant_id,sla_resolution_deadline) 查询，本文件补齐。
-- 该脚本可重复执行；对 RLS/分区无破坏。

CREATE INDEX IF NOT EXISTS idx_tickets_tenant_status_updated
    ON tickets (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_tickets_tenant_assignee_status
    ON tickets (tenant_id, assignee_id, status);

CREATE INDEX IF NOT EXISTS idx_tickets_tenant_sla_resolution
    ON tickets (tenant_id, sla_resolution_deadline);
