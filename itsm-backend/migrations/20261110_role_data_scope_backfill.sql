-- 20261110_role_data_scope_backfill
-- 目的：让 roles.data_scope 成为可信的角色数据范围来源。
--
-- 背景：data_scope 列默认值为 all，历史 seeder 创建内置角色时未显式写入，
-- 导致 end_user/agent 等角色在库中也是 all。若直接按角色 data_scope 取数，
-- 会让普通用户看到全租户单据。本迁移只收敛「保留内置角色 code」：
--   - owner：agent/technician/end_user/guest 及既有非管理岗位角色
--   - department：部门/一线/二线支持角色
--   - all：管理类与 MSP/provider 角色保持 all（不在此收窄）
--   - 自定义角色 code 不受影响
--
-- 幂等：重复执行为 no-op；只把比规范值更宽的 all/department 收敛。
-- 回滚：20261110_role_data_scope_backfill_down.sql 恢复内置角色为 all（旧默认）。

-- 1) 补齐内置角色 is_system 标记（运行时 ResolveForActor 的内置判定依赖）。
UPDATE roles
SET is_system = true,
    updated_at = now()
WHERE code IN (
    'super_admin', 'admin', 'manager', 'it_admin', 'security_admin',
    'agent', 'technician', 'sysadmin', 'end_user', 'guest',
    'it_director', 'ops_director', 'audit_admin', 'ops_manager', 'ops_engineer',
    'dba', 'network_eng', 'sd_manager', 'l1_support', 'l2_support', 'l3_expert',
    'change_manager', 'service_catalog_admin', 'dept_manager', 'provider_tenant_admin',
    'msp_viewer', 'msp_tech', 'msp_specialist', 'msp_manager', 'msp_admin'
)
AND is_system = false;

-- 2) owner 角色：比规范值更宽（all/department）时收敛为 owner。
UPDATE roles
SET data_scope = 'owner',
    updated_at = now()
WHERE code IN (
    'agent', 'technician', 'end_user', 'guest',
    'it_admin', 'security_admin', 'audit_admin',
    'it_director', 'ops_director', 'ops_manager', 'ops_engineer',
    'dba', 'network_eng', 'l3_expert', 'change_manager', 'service_catalog_admin'
)
AND data_scope IN ('all', 'department');

-- 3) department 角色：仅把 all 收敛为 department；已是 owner 的保留（更窄）。
UPDATE roles
SET data_scope = 'department',
    updated_at = now()
WHERE code IN ('dept_manager', 'sd_manager', 'l1_support', 'l2_support')
AND data_scope = 'all';
