-- IP-P1-3 回滚：下线组织关联子表（先解 FK，再删表；索引随表删除）。
-- 兼容读仍走 users 单值 FK（department_id / team_users / group_members），无需回滚数据。

ALTER TABLE IF EXISTS user_tenant_membership_orgs
  DROP CONSTRAINT IF EXISTS fk_membership_org_membership;
DROP TABLE IF EXISTS user_tenant_membership_orgs;
