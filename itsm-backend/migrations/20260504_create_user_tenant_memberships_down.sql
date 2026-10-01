-- 回滚 IP-P1-1（表可下线；读路径回退 home+allocation 计算）
-- 注意：audit_logs.membership_id 的历史回填值保留（仅列可空，不随表删除回滚）。
DROP TABLE IF EXISTS user_tenant_memberships;
