-- 演示库一次性数据校正：演示用户「部门」与组织对齐
-- 背景：审批待办（manager 层级）按「申请人部门 == 审批人部门」过滤；
--       早期演示数据把「岗位」写进了 department 字段，导致部门经理看不到本部门服务请求待办。
-- 幂等：按用户名覆盖为规范部门值。
\pset pager off

UPDATE users SET department = '服务交付部', updated_at = now()
WHERE username IN ('demo.msp.manager','demo.msp.agent1','demo.msp.agent2');

UPDATE users SET department = '信息技术部', updated_at = now()
WHERE username IN ('demo.mfg.admin','demo.mfg.agent','demo.mfg.itadmin','demo.mfg.secadmin');
UPDATE users SET department = '生产部', updated_at = now()
WHERE username IN ('demo.mfg.lead','demo.mfg.user');

UPDATE users SET department = '信息科', updated_at = now()
WHERE username IN ('demo.med.admin','demo.med.agent','demo.med.itadmin','demo.med.secadmin');
UPDATE users SET department = '门诊部', updated_at = now()
WHERE username IN ('demo.med.lead','demo.med.user');

UPDATE users SET department = '信息中心', updated_at = now()
WHERE username IN ('demo.edu.admin','demo.edu.agent','demo.edu.itadmin','demo.edu.secadmin');
UPDATE users SET department = '教务处', updated_at = now()
WHERE username IN ('demo.edu.lead','demo.edu.user');

SELECT tenant_id, username, role, department FROM users
WHERE username LIKE 'demo.%' ORDER BY tenant_id, username;
