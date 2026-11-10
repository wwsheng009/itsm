# 数据库迁移回滚指南

> Status: current for the rollback process; the migration index in §2 is historical.
> 更新：2026-11-09。2026-10/11 新增迁移的回滚覆盖见 §7。

> 本文档配套 `itsm-backend/migrations/` 下所有 `YYYYMMDD_xxx.sql` 文件使用。
> 每个迁移文件头部应包含 `ROLLBACK:` 注释指向回滚方案（手写或脚本生成）。
> v1.0 GA 准入要求：所有 `2026-06-01` 之后的迁移必须有可执行的回滚 SQL。

## 1. 命名约定

| 文件类型 | 命名 | 示例 |
|----------|------|------|
| 正向迁移 | `YYYYMMDD_xxx.sql` | `20260621_process_definition_approval_sla_config.sql` |
| 回滚迁移 | `YYYYMMDD_xxx_down.sql` | `20260621_process_definition_approval_sla_config_down.sql` |
| 文件头部注释 | `-- ROLLBACK: <down-file> or <manual SQL>` | `-- ROLLBACK: 20260621_xxx_down.sql` |

## 2. 近期迁移回滚索引

| 迁移文件 | 风险 | 回滚方式 |
|----------|------|----------|
| `20260621_process_definition_approval_sla_config.sql` | 中 | DROP COLUMN approval_config / sla_config（数据会丢失，需先备份） |
| `20260620_create_marketplace.sql` | 中 | DROP TABLE marketplace_*（级联） |
| `20260620_process_routing_enhancement.sql` | 中 | DROP COLUMN |
| `20260617_role_is_active.sql` | 低 | DROP COLUMN is_active |
| `20260616_process_instance_version.sql` | 中 | DROP COLUMN version |
| `20260616_security_role_permissions.sql` | 高 | DROP TABLE role_permissions CASCADE（会清空所有 RBAC 配置） |
| `20260614_ci_relationship_tenant_id.sql` | 中 | DROP COLUMN tenant_id（必须先确保所有行 tenant_id NOT NULL） |
| `20260609_create_change_approvals.sql` | 高 | DROP TABLE change_approvals CASCADE |
| `20260609_add_resolution_category.sql` | 低 | DROP COLUMN |
| `20260607_create_ticket_workflow_records.sql` | 高 | DROP TABLE ticket_workflow_records CASCADE |
| `20260523_create_change_pir.sql` | 高 | DROP TABLE change_pir CASCADE |
| `20260501_enable_rbac_from_db.sql` | 极高 | DROP TABLE endpoint_acls CASCADE（会清空所有 ACL） |
| `20260501_rbac_endpoint_acls.sql` | 极高 | DROP TABLE endpoint_acls CASCADE |

## 3. 回滚执行顺序

回滚必须**按时间倒序**执行，从最新到最旧：

```bash
# 1. 先停服务
docker compose down

# 2. 倒序执行回滚
PGPASSWORD=$DB_PASSWORD psql -h $DB_HOST -U $DB_USER -d $DB_NAME -f migrations/20260621_xxx_down.sql
PGPASSWORD=$DB_PASSWORD psql -h $DB_HOST -U $DB_USER -d $DB_NAME -f migrations/20260620_xxx_down.sql
# ...

# 3. 启动服务
docker compose up -d
```

## 4. 回滚脚本模板

```sql
-- ROLLBACK FOR: 20260621_process_definition_approval_sla_config.sql
-- Generated: 2026-06-27
-- WARNING: 会清空 approval_config / sla_config 数据，操作前请备份。

BEGIN;

-- 备份（可选，正式环境必做）
CREATE TABLE IF NOT EXISTS process_definitions_bak_20260621 AS
SELECT * FROM process_definitions;

-- 移除列
ALTER TABLE process_definitions DROP COLUMN IF EXISTS approval_config;
ALTER TABLE process_definitions DROP COLUMN IF EXISTS sla_config;

COMMIT;
```

## 5. 强制规则

- **所有 `2026-06-01` 之后的迁移必须配套 `_down.sql`**（PR #01 合入时同步补齐）。
- **不兼容变更（ALTER TYPE / DROP COLUMN 含数据）必须先备份再回滚**。
- **跨 PR 合入时回滚脚本必须与正向迁移同 PR 提交**。
- **生产环境回滚需要 DBA + 架构师双签字**。

## 6. 自动化校验

CI 门禁 `ga-gate.yml` 在 G3 阶段执行：

```bash
# 检查所有 2026-06-01 之后的迁移是否有 _down.sql
for f in migrations/202606*.sql; do
  if [ ! -f "${f%.sql}_down.sql" ]; then
    echo "::error::Missing rollback: ${f%.sql}_down.sql"
    exit 1
  fi
done
```

## 7. 当前回滚覆盖（2026-11-09）

近期迁移中以下文件暂无同名 `_down.sql`，发布证据必须登记为 **forward-fix**：

- `20261018_add_ai_help_coverage_menu.sql`（菜单数据）
- `20261018_add_ai_observability_menu.sql`（菜单数据）
- `20261018_ai_telemetry_events.sql`（新建表）
- `20261018_ai_telemetry_events_filters.sql`（索引/字段）
- `20261018_ai_telemetry_events_retention.sql`（索引）
- `20261018m_msp_help_space.sql`
- `20261018n_ai_doc_todo_states.sql`
- `20261029_project_management_progress_backfill.sql`（数据回填）

其余 2026-10/11 迁移（项目 P0–P2、客户主数据 P0–P2、合同 C1、SLA 快照、
问题调查、HTTP 幂等、PAT 等）均有 `_down.sql`。新迁移合入时仍应遵循：

- 2026-06-01 之后的迁移必须配套 `_down.sql`，或明确登记 forward-fix 理由；
- 不兼容变更（DROP COLUMN / ALTER TYPE / 数据回填）必须先备份；
- 生产回滚需 DBA + 架构师双签字。

---

**文档版本**：v1.0-rc.1
**维护人**：DBA + 架构师
