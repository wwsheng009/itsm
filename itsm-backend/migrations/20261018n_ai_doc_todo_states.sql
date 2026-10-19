-- =============================================================================
-- H1-01/H1-03/H1-04 条件项闭环：文档待办「认领/忽略」状态持久化
-- （方案 §5.6 自维护文档闭环：待办队列的运营归属与静默）。
--
-- 口径：
--   - 每租户每 pageKey 至多一行（UNIQUE (tenant_id, page_key)）；
--   - state ∈ {claimed, ignored}；「恢复待办」= 删除行（缺省无行即 open，
--     避免把 open 的空语义写进表里制造噪声）；
--   - updated_by 记录最近操作人（用户 id），updated_at 为最近操作时间；
--   - 行不含待办内容本身（缺口/账本为派生数据），仅存运营状态。
--
-- RLS：database/rls/migrations/017_ai_doc_todo_states_policies.sql（tenant 隔离，只 ENABLE 不 FORCE）。
-- 应用（无 psql 环境）：go run -tags rlsapply ./cmd/rls-apply -files 017_ai_doc_todo_states_policies.sql
-- =============================================================================

CREATE TABLE IF NOT EXISTS ai_doc_todo_states (
    id         BIGSERIAL     PRIMARY KEY,
    tenant_id  BIGINT        NOT NULL,
    page_key   VARCHAR(128)  NOT NULL,
    state      VARCHAR(16)   NOT NULL,
    updated_by BIGINT        NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT uk_ai_doc_todo_states_tenant_page UNIQUE (tenant_id, page_key),
    CONSTRAINT ck_ai_doc_todo_states_state CHECK (state IN ('claimed', 'ignored'))
);

CREATE INDEX IF NOT EXISTS idx_ai_doc_todo_states_tenant_state
    ON ai_doc_todo_states (tenant_id, state);