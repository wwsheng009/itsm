-- 演示库一次性清理：删除早期幂等缺陷产生的重复服务请求（id 10/11/12）
-- 背景：fix_sr_titles.sql 调整标题后缀后，旧版种子器按完整标题匹配 → 重复建单；
--       种子器已改为「标题词干」匹配，本脚本清理已产生的重复行（含其审批记录）。
-- 幂等：删除后重复执行无副作用。
\pset pager off

DELETE FROM service_request_approvals WHERE request_id IN (10, 11, 12);
DELETE FROM service_requests WHERE id IN (10, 11, 12);

SELECT tenant_id, id, status, title FROM service_requests ORDER BY tenant_id, id;
