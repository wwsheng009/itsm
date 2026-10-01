-- =============================================================================
-- IP-P1-3 巡检：user_tenant_membership_orgs 回填与一致性（期望全部 0 差异）
-- 用法：psql "$DATABASE_URL" -f scripts/msp/verify-membership-orgs-backfill.sql
-- 部署后执行并留档；第 1 节为契约断言，第 3–7 节为数据一致性 0 差异口径。
-- =============================================================================

\echo '== 1. 复合 FK 存在性（(membership_id, tenant_id) → user_tenant_memberships(id, tenant_id)） =='
SELECT conname, convalidated
FROM pg_constraint
WHERE conrelid = 'user_tenant_membership_orgs'::regclass
  AND conname = 'fk_membership_org_membership';

\echo '== 2. 行数分布（org_type × status；参考） =='
SELECT org_type, status, count(*) AS rows
FROM user_tenant_membership_orgs
GROUP BY 1, 2
ORDER BY 1, 2;

\echo '== 3. 跨租户错配（org.tenant_id <> membership_orgs.tenant_id；期望 0） =='
SELECT count(*) AS cross_tenant_mismatches
FROM (
  SELECT mo.id, mo.tenant_id, d.tenant_id AS org_tenant
  FROM user_tenant_membership_orgs mo
  JOIN departments d ON mo.org_type = 'department' AND d.id = mo.org_id
  UNION ALL
  SELECT mo.id, mo.tenant_id, t.tenant_id
  FROM user_tenant_membership_orgs mo
  JOIN teams t ON mo.org_type = 'team' AND t.id = mo.org_id
  UNION ALL
  SELECT mo.id, mo.tenant_id, g.tenant_id
  FROM user_tenant_membership_orgs mo
  JOIN groups g ON mo.org_type = 'group' AND g.id = mo.org_id
  UNION ALL
  SELECT mo.id, mo.tenant_id, p.tenant_id
  FROM user_tenant_membership_orgs mo
  JOIN projects p ON mo.org_type = 'project' AND p.id = mo.org_id
) x
WHERE x.org_tenant <> x.tenant_id;

\echo '== 4. 悬挂组织（org_id 在对应组织表不存在；期望 0） =='
SELECT count(*) AS dangling_org_rows
FROM user_tenant_membership_orgs mo
WHERE NOT EXISTS (
  SELECT 1 FROM departments d WHERE mo.org_type = 'department' AND d.id = mo.org_id
  UNION ALL
  SELECT 1 FROM teams t WHERE mo.org_type = 'team' AND t.id = mo.org_id
  UNION ALL
  SELECT 1 FROM groups g WHERE mo.org_type = 'group' AND g.id = mo.org_id
  UNION ALL
  SELECT 1 FROM projects p WHERE mo.org_type = 'project' AND p.id = mo.org_id
);

\echo '== 5. 存活主组织重复（同 membership + org_type 多个 is_primary；期望 0） =='
SELECT membership_id, org_type, count(*) AS primary_rows
FROM user_tenant_membership_orgs
WHERE deleted_at IS NULL AND is_primary
GROUP BY 1, 2
HAVING count(*) > 1;

\echo '== 6. 遗留单值 FK 未回填（users 有 department_id/team_users/group_members，但 home membership 无对应行；期望 0） =='
SELECT count(*) AS missing_legacy_backfill
FROM users u
JOIN user_tenant_memberships m
  ON m.user_id = u.id AND m.tenant_id = u.tenant_id AND m.deleted_at IS NULL
WHERE (u.department_id IS NOT NULL OR u.team_users IS NOT NULL OR u.group_members IS NOT NULL)
  AND NOT EXISTS (
    SELECT 1
    FROM user_tenant_membership_orgs mo
    WHERE mo.membership_id = m.id
      AND (
        (mo.org_type = 'department' AND mo.org_id = u.department_id) OR
        (mo.org_type = 'team'       AND mo.org_id = u.team_users) OR
        (mo.org_type = 'group'      AND mo.org_id = u.group_members)
      )
  );

\echo '== 7. 软删/状态不一致（deleted_at 非空但 status=active；期望 0） =='
SELECT count(*) AS deleted_but_active
FROM user_tenant_membership_orgs
WHERE deleted_at IS NOT NULL AND status = 'active';

\echo '== 8. 已过期仍存活（expires_at < now() AND deleted_at IS NULL；参考，读取路径按 now 判定） =='
SELECT count(*) AS expired_live_rows
FROM user_tenant_membership_orgs
WHERE expires_at IS NOT NULL AND expires_at < now() AND deleted_at IS NULL;
