-- IP-P1-3：user_tenant_membership_orgs 组织关联子表（实施方案 §4.0-A 契约）
-- 幂等可重放：建表/索引 IF NOT EXISTS；复合 FK 用 DROP IF EXISTS + ADD 保证存在；
-- 回填 ON CONFLICT DO NOTHING + 同租户 JOIN 预过滤（跨租户组织不入表）。
-- 本批只加表与回填 + 应用层双校验；users 单值 FK（department_id/team_users/group_members）保留兼容读。

CREATE TABLE IF NOT EXISTS user_tenant_membership_orgs (
  id            bigserial PRIMARY KEY,
  membership_id bigint NOT NULL,
  tenant_id     int    NOT NULL REFERENCES tenants(id),
  org_type      varchar(16) NOT NULL,
  org_id        bigint NOT NULL,
  role_id       int NULL REFERENCES roles(id),
  is_primary    boolean NOT NULL DEFAULT false,
  status        varchar(16) NOT NULL DEFAULT 'active',
  expires_at    timestamptz NULL,
  deleted_at    timestamptz NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_membership_org_type   CHECK (org_type IN ('department','team','group','project')),
  CONSTRAINT ck_membership_org_status CHECK (status IN ('active','suspended')),
  CONSTRAINT uq_membership_org UNIQUE (membership_id, org_type, org_id)
);

CREATE INDEX IF NOT EXISTS idx_membership_org_scope
  ON user_tenant_membership_orgs (tenant_id, org_type, org_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_org_primary
  ON user_tenant_membership_orgs (membership_id, org_type)
  WHERE deleted_at IS NULL AND is_primary;

-- 复合 FK：组织归属与 membership 必须同租户（DB 层强约束；§4.0-A）。
-- 采用 DROP IF EXISTS + ADD 形式，保证 ent auto-migrate 先行建表时约束仍然存在；重放安全。
ALTER TABLE user_tenant_membership_orgs
  DROP CONSTRAINT IF EXISTS fk_membership_org_membership;
ALTER TABLE user_tenant_membership_orgs
  ADD CONSTRAINT fk_membership_org_membership
  FOREIGN KEY (membership_id, tenant_id)
  REFERENCES user_tenant_memberships (id, tenant_id);

-- ============ 回填 1：department（users.department_id，单值兼容读） ============
INSERT INTO user_tenant_membership_orgs
  (membership_id, tenant_id, org_type, org_id, is_primary, status, created_at, updated_at)
SELECT m.id, m.tenant_id, 'department', u.department_id, true, 'active', now(), now()
FROM users u
JOIN user_tenant_memberships m
  ON m.user_id = u.id AND m.deleted_at IS NULL
JOIN departments d ON d.id = u.department_id
WHERE u.department_id IS NOT NULL
  AND d.tenant_id = m.tenant_id
ON CONFLICT (membership_id, org_type, org_id) DO NOTHING;

-- ============ 回填 2：team（users.team_users，单值兼容读） ============
INSERT INTO user_tenant_membership_orgs
  (membership_id, tenant_id, org_type, org_id, is_primary, status, created_at, updated_at)
SELECT m.id, m.tenant_id, 'team', u.team_users, true, 'active', now(), now()
FROM users u
JOIN user_tenant_memberships m
  ON m.user_id = u.id AND m.deleted_at IS NULL
JOIN teams t ON t.id = u.team_users
WHERE u.team_users IS NOT NULL
  AND t.tenant_id = m.tenant_id
ON CONFLICT (membership_id, org_type, org_id) DO NOTHING;

-- ============ 回填 3：group（users.group_members，单值兼容读） ============
INSERT INTO user_tenant_membership_orgs
  (membership_id, tenant_id, org_type, org_id, is_primary, status, created_at, updated_at)
SELECT m.id, m.tenant_id, 'group', u.group_members, true, 'active', now(), now()
FROM users u
JOIN user_tenant_memberships m
  ON m.user_id = u.id AND m.deleted_at IS NULL
JOIN groups g ON g.id = u.group_members
WHERE u.group_members IS NOT NULL
  AND g.tenant_id = m.tenant_id
ON CONFLICT (membership_id, org_type, org_id) DO NOTHING;

-- project：当前无成员列/边（project 仅 department 归属），回填留空；
-- 多组织 project 归属由后续写入路径（membership orgs API）产生。
