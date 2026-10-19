-- 2026-10-18 添加帮助覆盖率看板菜单（H1-04 帮助闭环）。
--
-- 口径：与 seeder 的 AI 助手子菜单（ParentPath=/ai/chat）保持一致，全部租户补齐；
-- 权限码复用 ai:read（与 /ai/observability、/ai/help/* 同源）。
-- 幂等性：menus 表无 (tenant_id, path) 唯一约束（见 20261018_add_ai_observability_menu.sql 说明），
-- 采用 NOT EXISTS 守卫保证重复执行安全。

INSERT INTO menus (name, path, icon, parent_id, permission_code, sort_order, tenant_id, is_visible, is_enabled)
SELECT '帮助覆盖率', '/ai/help-coverage', 'FileText', m.id, 'ai:read', 118, m.tenant_id, true, true
FROM menus m
WHERE m.path = '/ai/chat'
  AND NOT EXISTS (
      SELECT 1 FROM menus existing
      WHERE existing.tenant_id = m.tenant_id AND existing.path = '/ai/help-coverage'
  );