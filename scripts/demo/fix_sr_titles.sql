-- 演示库一次性数据校正：服务请求标题与真实审批层级对齐
-- 背景：早期种子规格标题为「（三级已批）」，实际仅推进到两级（BPMN 任务操作人鉴权限制，见演示文档）；
--       已审批服务请求不可再编辑/删除，故仅能在演示库直改标题。
UPDATE service_requests
SET title = replace(title, '（三级已批）', '（两级已批）'),
    updated_at = now()
WHERE title LIKE '[DEMO]%'
  AND title LIKE '%（三级已批）';

-- 标题与实际状态对齐：个别实例的审批被流程任务桥接拒绝（500），仍停留在 submitted，
-- 标题不应声称「已批」——改为「待审批」表述。
UPDATE service_requests
SET title = replace(title, '（经理已批）', '（待经理审批）'),
    updated_at = now()
WHERE title LIKE '[DEMO]%'
  AND title LIKE '%（经理已批）'
  AND status = 'submitted';

-- 按实际状态逐条对齐「两级/已完成」类标题（个别实例的层级审批被 BPMN 桥接拒绝，状态未推进）。
UPDATE service_requests SET title = '[DEMO] 软件安装申请（经理已批）', updated_at = now()
WHERE id = 3 AND status = 'manager_approved';
UPDATE service_requests SET title = '[DEMO] 数据导出授权申请（待经理审批）', updated_at = now()
WHERE id = 6 AND status = 'submitted';
UPDATE service_requests SET title = '[DEMO] 校园网端口申请（两级已批）', updated_at = now()
WHERE id = 9 AND status = 'it_approved';

SELECT tenant_id, id, status, title FROM service_requests ORDER BY tenant_id, id;
