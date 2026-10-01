-- IP-P1-1：user_tenant_memberships 表（目标架构 §3.2 / 实施方案 §6.1 P1 契约）
-- 幂等可重放：建表/索引 IF NOT EXISTS；回填 ON CONFLICT DO NOTHING + NOT EXISTS 预过滤。
-- 本批只加表与回填，读路径仍回退 home+allocation 计算（切换读取归 IP-P1-2）。

CREATE TABLE IF NOT EXISTS user_tenant_memberships (
  id            bigserial PRIMARY KEY,
  user_id       bigint NOT NULL REFERENCES users(id),
  tenant_id     int    NOT NULL REFERENCES tenants(id),
  account_kind  varchar(16) NOT NULL DEFAULT 'customer',
  subject_type  varchar(16) NOT NULL DEFAULT 'user',
  source        varchar(16) NOT NULL,
  role_id       int NULL REFERENCES roles(id),
  msp_role      varchar(32) NULL,
  allocation_id bigint NULL REFERENCES msp_allocations(id),
  status        varchar(16) NOT NULL DEFAULT 'active',
  is_default    boolean NOT NULL DEFAULT false,
  expires_at    timestamptz NULL,
  invited_by    bigint NULL REFERENCES users(id),
  joined_at     timestamptz NULL,
  deassigned_at timestamptz NULL,
  deleted_at    timestamptz NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_membership_account_kind CHECK (account_kind IN ('customer','provider','platform')),
  CONSTRAINT ck_membership_subject_type CHECK (subject_type IN ('user')),
  CONSTRAINT ck_membership_source CHECK (source IN ('home','allocation','platform','invite','migration')),
  CONSTRAINT ck_membership_status CHECK (status IN ('active','suspended'))
);

-- 3 个部分唯一索引（§3.2 关键约束）
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_live
  ON user_tenant_memberships (user_id, tenant_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_default
  ON user_tenant_memberships (user_id) WHERE deleted_at IS NULL AND is_default AND status = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_single_scope
  ON user_tenant_memberships (user_id) WHERE deleted_at IS NULL AND status = 'active' AND account_kind = 'customer';

-- 复合 FK 目标（供 IP-P1-3 的 user_tenant_membership_orgs 子表使用，§4.0-A）
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_id_tenant
  ON user_tenant_memberships (id, tenant_id);

-- 查询辅助索引
CREATE INDEX IF NOT EXISTS idx_membership_tenant_status ON user_tenant_memberships (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_membership_user_status   ON user_tenant_memberships (user_id, status);
CREATE INDEX IF NOT EXISTS idx_membership_allocation    ON user_tenant_memberships (allocation_id);

-- ============ 回填 1：home membership（source=home，is_default=true） ============
INSERT INTO user_tenant_memberships
  (user_id, tenant_id, account_kind, subject_type, source, role_id, msp_role,
   status, is_default, joined_at, created_at, updated_at)
SELECT
  u.id,
  u.tenant_id,
  CASE
    WHEN t.type IN ('msp_customer','saas_customer','customer')
         AND (u.msp_role IS NULL OR u.msp_role = 'customer_user') THEN 'customer'
    WHEN t.type IN ('msp_provider','msp') OR u.msp_role IN ('provider_admin','provider_agent') THEN 'provider'
    ELSE 'platform'
  END AS account_kind,
  'user',
  'home',
  r.id,
  CASE
    WHEN u.msp_role = 'provider_admin' THEN 'msp_manager'
    WHEN u.msp_role = 'provider_agent' THEN 'msp_tech'
    ELSE NULL
  END AS msp_role,
  'active',
  true,
  u.created_at,
  now(),
  now()
FROM users u
JOIN tenants t ON t.id = u.tenant_id
LEFT JOIN LATERAL (
  SELECT rr.id
  FROM roles rr
  WHERE rr.tenant_id = u.tenant_id
    AND rr.code = CASE
      WHEN u.msp_role = 'provider_admin' THEN 'msp_manager'
      WHEN u.msp_role = 'provider_agent' THEN 'msp_tech'
      ELSE u.role::text
    END
  ORDER BY rr.id
  LIMIT 1
) r ON true
WHERE NOT EXISTS (
  SELECT 1 FROM user_tenant_memberships m
  WHERE m.user_id = u.id AND m.tenant_id = u.tenant_id AND m.deleted_at IS NULL
)
ON CONFLICT DO NOTHING;

-- ============ 回填 2：有效 allocation → 服务方客户作用域（source=allocation） ============
-- role 映射（D10/Q7 预设）：specialist→msp_specialist；primary/backup/其他→msp_tech；
-- role_id 取客户租户内同名角色行（未 seed 则为 NULL，进入巡检清单）。
INSERT INTO user_tenant_memberships
  (user_id, tenant_id, account_kind, subject_type, source, role_id, msp_role,
   allocation_id, status, is_default, joined_at, created_at, updated_at)
SELECT
  a.msp_user_id,
  a.customer_tenant_id,
  'provider',
  'user',
  'allocation',
  r.id,
  CASE WHEN a.role = 'specialist' THEN 'msp_specialist' ELSE 'msp_tech' END,
  a.id,
  'active',
  false,
  a.assigned_at,
  now(),
  now()
FROM msp_allocations a
JOIN users u ON u.id = a.msp_user_id
LEFT JOIN LATERAL (
  SELECT rr.id
  FROM roles rr
  WHERE rr.tenant_id = a.customer_tenant_id
    AND rr.code = CASE WHEN a.role = 'specialist' THEN 'msp_specialist' ELSE 'msp_tech' END
  ORDER BY rr.id
  LIMIT 1
) r ON true
WHERE a.deassigned_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM user_tenant_memberships m
    WHERE m.user_id = a.msp_user_id
      AND m.tenant_id = a.customer_tenant_id
      AND m.deleted_at IS NULL
  )
ON CONFLICT DO NOTHING;

-- ============ 回填 3：audit_logs.membership_id（IP-P0-10 遗留；按 actor home membership） ============
UPDATE audit_logs al
SET membership_id = m.id
FROM user_tenant_memberships m
JOIN users u ON u.id = m.user_id
WHERE al.membership_id IS NULL
  AND m.source = 'home'
  AND m.deleted_at IS NULL
  AND m.tenant_id = al.tenant_id
  AND al.actor_account = u.username;
