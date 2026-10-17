# P0 数据 DDL 草案（Ent Schema + 迁移兜底 + RLS）

> 状态：**Draft v1.0（2026-10-17）**
> 范围：P0 表结构（P1/P2 表仅列名与要点）
> 约定依据：[02 数据模型](../02-data-model.md)、[P0 集成契约](./p0-integration-contracts.md)
> 本仓惯例：**表由 ent Schema.Create 先行创建**，`itsm-backend/migrations/*.sql` 仅做幂等兜底（ADD COLUMN / CREATE INDEX IF NOT EXISTS），RLS 策略独立放 `database/rls/migrations/`。

---

## 1. 类型与命名约定

- ID：`BIGINT`（ent `int` 生成口径以 entc 为准，迁移兜底按 BIGINT 兼容）；
- 时间：`TIMESTAMPTZ`；软删 `deleted_at TIMESTAMPTZ NULL`；乐观锁 `version INTEGER NOT NULL DEFAULT 1`；
- 状态/枚举：`VARCHAR(16~32)` + 服务层受控（不建 PG enum，保持与既有表一致）；
- 所有业务表带 `tenant_id BIGINT NOT NULL` + RLS（`ENABLE` 不 `FORCE`，策略名 `tenant_isolation_<table>`）；
- 唯一性一律租户内/项目内收敛；存活行唯一用**部分唯一索引**（先例：`uq_membership_live`、`uk_msp_allocation_active`）。

## 2. `projects` 增列（P0）

| 列 | 类型 | 说明 |
|---|---|---|
| `type` | VARCHAR(16) NOT NULL DEFAULT 'internal' | `internal\|improvement\|delivery` |
| `parent_project_id` | BIGINT NULL | 父子项目 |
| `health` | VARCHAR(8) NOT NULL DEFAULT 'green' | `green\|yellow\|red` |
| `progress` | INTEGER NOT NULL DEFAULT 0 | 0–100 |
| `actual_start` / `actual_end` | TIMESTAMPTZ NULL | 实际起止 |
| `closed_at` | TIMESTAMPTZ NULL | 结项时间 |
| `owner_team_id` | BIGINT NULL | 团队归口（契约 §1-2） |
| `version` | INTEGER NOT NULL DEFAULT 1 | 乐观锁 |
| `deleted_at` | TIMESTAMPTZ NULL | 软删（DELETE 语义） |

> `status` 存量兼容：保留原值，读侧映射到受控集合 `planning|active|suspended|completed|cancelled`；写侧只接受受控值。
> **不新增** `customer_tenant_id`（D-01）。
> 搜索：`q` 命中 `code`（唯一索引前缀）与 `name`（ILIKE）；P1 评估 `pg_trgm` 索引提升模糊搜索性能（审查 DR-11）。

```sql
-- 兜底（幂等）
ALTER TABLE projects ADD COLUMN IF NOT EXISTS type VARCHAR(16) NOT NULL DEFAULT 'internal';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS parent_project_id BIGINT;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS health VARCHAR(8) NOT NULL DEFAULT 'green';
ALTER TABLE projects ADD COLUMN IF NOT EXISTS progress INTEGER NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS actual_start TIMESTAMPTZ;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS actual_end TIMESTAMPTZ;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS owner_team_id BIGINT;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_projects_tenant_status ON projects (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_projects_tenant_type   ON projects (tenant_id, type);
CREATE INDEX IF NOT EXISTS idx_projects_owner_team    ON projects (tenant_id, owner_team_id);
CREATE INDEX IF NOT EXISTS idx_projects_tenant_manager ON projects (tenant_id, manager_id);
```

## 3. 业务单据增列（P0）

五表：`tickets` / `incidents` / `problems` / `changes` / `service_requests`：

```sql
ALTER TABLE tickets          ADD COLUMN IF NOT EXISTS project_id BIGINT;
ALTER TABLE incidents        ADD COLUMN IF NOT EXISTS project_id BIGINT;
ALTER TABLE problems         ADD COLUMN IF NOT EXISTS project_id BIGINT;
ALTER TABLE changes          ADD COLUMN IF NOT EXISTS project_id BIGINT;
ALTER TABLE service_requests ADD COLUMN IF NOT EXISTS project_id BIGINT;
-- 部分索引（仅关联行）
CREATE INDEX IF NOT EXISTS idx_tickets_project          ON tickets (tenant_id, project_id)          WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_incidents_project        ON incidents (tenant_id, project_id)        WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_problems_project         ON problems (tenant_id, project_id)         WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_changes_project          ON changes (tenant_id, project_id)          WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_service_requests_project ON service_requests (tenant_id, project_id) WHERE project_id IS NOT NULL;
-- survey_response（P1 CSAT，契约 §1-5）
ALTER TABLE survey_responses ADD COLUMN IF NOT EXISTS project_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_survey_responses_project ON survey_responses (tenant_id, project_id) WHERE project_id IS NOT NULL;
```

> **落地状态（2026-10-21）**：五类单据 `project_id` 随 P0 落地；`survey_responses.project_id` 在 P0 批次遗漏，**已随 PE-15 补齐**（ent schema + `migrations/20261021_project_management_pe15.sql` 兜底 + 部分索引）；CSAT 答卷经该列关联项目。

## 4. 新表（P0）

### 4.1 `project_members`

| 列 | 类型 | 说明 |
|---|---|---|
| tenant_id / project_id / user_id | BIGINT NOT NULL | user_id 必须为该租户活跃 membership |
| project_role | VARCHAR(16) NOT NULL DEFAULT 'member' | `pm\|member\|stakeholder` |
| status | VARCHAR(16) NOT NULL DEFAULT 'active' | `active\|removed` |
| joined_at | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| created_by / created_at / updated_at | BIGINT / TIMESTAMPTZ | |

```sql
CREATE UNIQUE INDEX IF NOT EXISTS uk_project_members_live
  ON project_members (tenant_id, project_id, user_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_project_members_tenant_user ON project_members (tenant_id, user_id);
```

### 4.2 `project_tasks`

| 列 | 类型 | 说明 |
|---|---|---|
| tenant_id / project_id | BIGINT NOT NULL | |
| parent_task_id | BIGINT NULL | WBS ≤3 层（服务层校验） |
| name | VARCHAR(255) NOT NULL | |
| description | TEXT NULL | |
| assignee_id | BIGINT NULL | 活跃 membership 才可指派 |
| status | VARCHAR(16) NOT NULL DEFAULT 'todo' | `todo\|in_progress\|blocked\|done\|cancelled` |
| priority | VARCHAR(16) NOT NULL DEFAULT 'medium' | `low\|medium\|high\|critical`（对齐 `common/validator.go` 词表） |
| plan_start / plan_end | TIMESTAMPTZ NULL | |
| actual_start / actual_end | TIMESTAMPTZ NULL | |
| progress | INTEGER NOT NULL DEFAULT 0 | |
| estimate_hours | DOUBLE PRECISION NULL（ent Float） | 估算工时（P2 展示） |
| actual_hours | DOUBLE PRECISION NULL（ent Float） | P2 落数；worklog 后置评估 |
| linked_ticket_id | BIGINT NULL | 任务→工单主链接（契约 §3.2） |
| sort_order | INTEGER NOT NULL DEFAULT 0 | |
| version / deleted_at / created_by / created_at / updated_at | | |

```sql
CREATE INDEX IF NOT EXISTS idx_project_tasks_project  ON project_tasks (tenant_id, project_id, status);
CREATE INDEX IF NOT EXISTS idx_project_tasks_assignee ON project_tasks (tenant_id, assignee_id, status);
CREATE INDEX IF NOT EXISTS idx_project_tasks_parent   ON project_tasks (parent_task_id);
```

### 4.3 `project_task_dependencies`（P1）

`task_id` / `depends_on_task_id` / `dep_type`（v1 仅 `fs`）/ `tenant_id` / 时间戳；

```sql
CREATE UNIQUE INDEX IF NOT EXISTS uk_project_task_deps ON project_task_dependencies (task_id, depends_on_task_id);
CREATE INDEX IF NOT EXISTS idx_project_task_deps_reverse ON project_task_dependencies (depends_on_task_id);
```

### 4.4 `project_milestones`（P1）

`tenant_id / project_id / name / due_date / status(planned|reached|missed) / owner_id / reached_at / created_at`；

```sql
CREATE INDEX IF NOT EXISTS idx_project_milestones ON project_milestones (tenant_id, project_id, status);
```

### 4.5 `project_baselines`（P1，不可变）

`tenant_id / project_id / version / snapshot JSONB / reason / created_by / created_at`；

```sql
CREATE UNIQUE INDEX IF NOT EXISTS uk_project_baselines ON project_baselines (project_id, version);
```

> 服务层禁止 UPDATE/DELETE；变更 = 插入新 version（契约 §4 计划基线）。

## 5. RLS 策略（新文件）

`database/rls/migrations/015_project_management_policies.sql`（编号以落地时实际序列为准，当前最新 014）：

```sql
-- 覆盖：project_members / project_tasks / project_task_dependencies / project_milestones / project_baselines
-- （projects 已由 003_business_tables_policies.sql 覆盖；survey_responses 暂不在 RLS 清单）
CREATE OR REPLACE FUNCTION get_current_tenant_id() RETURNS INTEGER AS $$
BEGIN
    RETURN NULLIF(current_setting('app.current_tenant', true), '')::INTEGER;
EXCEPTION WHEN invalid_text_representation THEN
    RETURN NULL;
END;
$$ LANGUAGE plpgsql STABLE;

DO $$
DECLARE
    t text;
    p text;
BEGIN
    FOREACH t IN ARRAY ARRAY['project_members','project_tasks','project_task_dependencies','project_milestones','project_baselines']
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NULL THEN
            RAISE NOTICE 'rls 015: skip % (table not found)', t;
            CONTINUE;
        END IF;
        p := 'tenant_isolation_' || t;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', p, t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (tenant_id = get_current_tenant_id()) WITH CHECK (tenant_id = get_current_tenant_id())',
            p, t);
        RAISE NOTICE 'rls 015: % enabled (policy %)', t, p;
    END LOOP;
END $$;
```

回滚文件 `015_project_management_policies_rollback.sql`：DROP POLICY + DISABLE RLS。

## 6. 登记清单（缺一不可）

| 位置 | 动作 |
|---|---|
| `cmd/rls-apply/main.go` `rlsManagedTables` | 追加 5 张新表 |
| `internal/schema/tenant_guard.go` | 登记新业务表（注意 `project_tags` 现有平台豁免先例，勿误伤） |
| `handlers/attachment/handler.go` | `attachmentHostPolicies`（§4.2 权威表）登记 `project`/`project_task` 宿主——**未登记会跳过宿主鉴权（DR-02）** |
| `common/pagination.go` | `domainAliases` 增 `projectTasks`/`projectMembers`/`projectMilestones`；`singularAliases` 增对应 `*Response → 复数` 映射 |
| `internal/authz/catalog.go` | **本批不新增权限码**（复用 `project:read/write/delete`；新码须走 catalog+codegen 完整流程） |
| 前端 | 路由/权限/菜单/i18n（见 API 与页面文档） |

## 7. 迁移文件与执行顺序

1. ent schema 变更 → `go generate`（参考 `docs/entc-codegen-runbook.md`）；
2. `itsm-backend/migrations/20261017_project_management_p0.sql`（本文件 §2/§3/§4 兜底语句合并）+ `..._down.sql`；
3. `database/rls/migrations/015_project_management_policies.sql` + rollback；
4. 执行与校验：
   ```bash
   cd itsm-backend
   go run -tags migrate main.go                                            # 表结构
   go run -tags rlsapply ./cmd/rls-apply -files 015_project_management_policies.sql
   go run -tags rlsapply ./cmd/rls-apply -verify                           # 受管表 RLS 状态校验
   ```

## 8. 验收要点

- [ ] entc 生成物与迁移兜底一致（列/索引无漂移）；
- [ ] 5 张新表 RLS 生效：`-verify` 通过，低权探针跨租户不可见；
- [ ] `projects`/五类单据/survey_responses 增列幂等可重放（重复执行无报错）；
- [ ] 存量数据零破坏：`status`/`progress` 默认值不改变既有行为；
- [ ] 分页别名生效：新资源响应可被前端列表正确解析。