-- IP-P1-3b 回滚：撤销租户内唯一索引，恢复 project.code 全局唯一。
-- 注意：若已存在跨租户同码项目，恢复全局唯一会失败（需先清理重复）；团队/组名恢复为无约束。

DROP INDEX IF EXISTS uq_project_tenant_code;
DROP INDEX IF EXISTS uq_group_tenant_name;
DROP INDEX IF EXISTS uq_team_tenant_name;

CREATE UNIQUE INDEX IF NOT EXISTS project_code ON projects (code);
