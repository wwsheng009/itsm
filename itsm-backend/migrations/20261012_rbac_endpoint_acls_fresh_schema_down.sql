-- 回滚说明：本迁移为 forward fix（重放 ACL/权限种子），不提供破坏性回滚。
-- 如需清理，请人工核对 endpoint_acls / permission_definitions / role_permissions 后处理。
SELECT 1;
