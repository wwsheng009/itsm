-- 2026-10-18 添加 AI 观测看板菜单（O0-03 观测性）。
--
-- 口径：与 seeder 的 AI 助手子菜单（ParentPath=/ai/chat）保持一致，全部租户补齐。
-- 幂等性：menus 表当前**没有** (tenant_id, path) 唯一约束（ent schema `Menu` 未声明索引，
-- 2026-10-18 真实 PG 冒烟发现既有 `ON CONFLICT (tenant_id, path)` 写法在真实表结构下会报
-- "no unique or exclusion constraint matching the ON CONFLICT specification"），
-- 因此采用 NOT EXISTS 守卫保证重复执行安全。

INSERT INTO menus (name, path, icon, parent_id, permission_code, sort_order, tenant_id, is_visible, is_enabled)
SELECT 'AI 观测看板', '/ai/observability', 'LineChart', m.id, 'ai:read', 117, m.tenant_id, true, true
FROM menus m
WHERE m.path = '/ai/chat'
  AND NOT EXISTS (
      SELECT 1 FROM menus existing
      WHERE existing.tenant_id = m.tenant_id AND existing.path = '/ai/observability'
  );