-- IP-P1-4：invitations 邀请表（实施方案 §4.0-C 契约冻结）
-- 幂等：CREATE ... IF NOT EXISTS。token_hash 存 sha256(token) hex，原始 token 不落库；
-- 同租户同邮箱至多一条 pending（lower(email) 部分唯一）；重发=旧邀请置 revoked 后新建。
-- tenant_id 纳入 RLS/guard（IP-P1-7 接入 policy）。

CREATE TABLE IF NOT EXISTS invitations (
  id             bigserial PRIMARY KEY,
  tenant_id      int NOT NULL REFERENCES tenants(id),
  token_hash     varchar(64) NOT NULL UNIQUE,
  email          varchar(255) NOT NULL,
  target_user_id int NULL REFERENCES users(id),
  role_id        int NOT NULL REFERENCES roles(id),
  msp_role       varchar(32) NULL,
  invited_by     int NOT NULL REFERENCES users(id),
  status         varchar(16) NOT NULL DEFAULT 'pending',
  expires_at     timestamptz NOT NULL,
  accepted_at    timestamptz NULL,
  revoked_at     timestamptz NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_invitation_status CHECK (status IN ('pending','accepted','revoked','expired'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_invitation_pending
  ON invitations (tenant_id, lower(email)) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_invitations_tenant_status
  ON invitations (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_invitations_expiry
  ON invitations (expires_at) WHERE status = 'pending';
