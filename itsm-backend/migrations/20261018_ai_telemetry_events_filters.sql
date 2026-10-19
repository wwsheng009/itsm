-- O0-03 观测看板：埋点事件补 role 维度（按角色过滤）。
--
-- 背景：看板验收要求"按 页面/角色/租户 过滤"。role 由服务端 RBAC 中间件提供
-- （handlers/ai/telemetry.go 从请求上下文写入），非前端上报，前端无法伪造。
-- 依赖：20261018_ai_telemetry_events.sql（先建表）。

ALTER TABLE ai_telemetry_events
    ADD COLUMN IF NOT EXISTS role VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS ai_telemetry_events_tenant_role_occurred_idx
    ON ai_telemetry_events (tenant_id, role, occurred_at);