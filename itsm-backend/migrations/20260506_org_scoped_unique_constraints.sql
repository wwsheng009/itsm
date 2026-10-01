-- IP-P1-3b：组织唯一约束租户化（团队名/组名/项目代码收敛到租户内）
-- 背景：group/team 名称此前无唯一约束（跨租户冲突 + 可枚举）；project.code 为全局唯一，
-- 跨租户同码冲突且阻断同名项目。本迁移将三者统一为 (tenant_id, ...) 唯一。
-- 幂等：DROP INDEX IF EXISTS + CREATE ... IF NOT EXISTS；ent 字段级唯一索引名为
-- <entity>_<field>（如 project_code）。
-- 注意：若存量数据已存在同租户重复，索引创建将失败；部署前先执行文件末的预检查询清理。

DROP INDEX IF EXISTS project_code;
DROP INDEX IF EXISTS projects_code;

CREATE UNIQUE INDEX IF NOT EXISTS uq_team_tenant_name
  ON teams (tenant_id, name) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_group_tenant_name
  ON groups (tenant_id, name);
CREATE UNIQUE INDEX IF NOT EXISTS uq_project_tenant_code
  ON projects (tenant_id, code);

-- ============ 部署前预检（手工执行；返回空集才可安全建索引） ============
-- SELECT tenant_id, name, count(*) FROM teams WHERE deleted_at IS NULL GROUP BY 1, 2 HAVING count(*) > 1;
-- SELECT tenant_id, name, count(*) FROM groups GROUP BY 1, 2 HAVING count(*) > 1;
-- SELECT tenant_id, code, count(*) FROM projects GROUP BY 1, 2 HAVING count(*) > 1;
