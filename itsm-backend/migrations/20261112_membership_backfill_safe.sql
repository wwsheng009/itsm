-- 20261112_membership_backfill_safe
-- Forward-only replacement for 20261111_membership_tenant_identity and
-- 20261112_membership_business_actors. Keep those published files byte-for-byte:
-- migration/superseded.go adopts ONLY unapplied versions; applied checksums stay intact.
-- This filename sorts BEFORE business_actors_reconcile / constraints_validate.
-- Deploy with the updated runner, not by replaying the old SQL files manually.
--
-- A historical membership (including a revoked invite, home, or migration row)
-- is evidence of an explicit lifecycle decision, NOT a missing home identity.
-- Never update, reactivate, delete, or re-role an existing membership here.
-- Already-applied unsafe backfills cannot be distinguished from legitimate
-- subsequent grants by source/timestamps; audit review is required, not inference.
-- No down migration: removing an identity may revoke legitimate access or destroy
-- actor evidence. Correct any reviewed historical data with an audited forward fix.

-- Serialize with membership grants/revocations while checking all historical rows.
LOCK TABLE user_tenant_memberships IN SHARE ROW EXCLUSIVE MODE;

WITH account_roles AS (
    SELECT u.id, u.tenant_id, t.type AS tenant_type,
        CASE lower(btrim(u.msp_role::text))
            WHEN 'provider_admin' THEN
                CASE WHEN u.is_bootstrap_admin THEN 'provider_tenant_admin' ELSE 'msp_manager' END
            WHEN 'provider_agent' THEN 'msp_tech'
            ELSE lower(btrim(u.role::text))
        END AS role_code,
        CASE lower(btrim(u.msp_role::text))
            WHEN 'provider_admin' THEN
                CASE WHEN u.is_bootstrap_admin THEN 'msp_admin' ELSE 'msp_manager' END
            WHEN 'provider_agent' THEN 'msp_tech'
            ELSE NULL
        END AS membership_msp_role
    FROM users u
    JOIN tenants t ON t.id = u.tenant_id
)
INSERT INTO user_tenant_memberships (
    user_id, tenant_id, account_kind, subject_type, source, role_id,
    msp_role, status, is_default, created_at, updated_at
)
SELECT a.id, a.tenant_id,
    CASE
        WHEN a.tenant_type IN ('msp_provider', 'msp') THEN 'provider'
        WHEN a.tenant_type IN ('msp_customer', 'saas_customer', 'customer') THEN 'customer'
        ELSE 'platform'
    END,
    'user', 'migration', r.id, a.membership_msp_role, 'active', true, NOW(), NOW()
FROM account_roles a
LEFT JOIN LATERAL (
    -- Same mapping as service/user_provisioning.go:provisionMembershipRoles.
    -- Missing tenant-local roles remain NULL for inspection, never fall back to admin.
    SELECT rr.id FROM roles rr
    WHERE rr.tenant_id = a.tenant_id AND rr.code = a.role_code
    ORDER BY rr.id
    LIMIT 1
) r ON true
WHERE NOT EXISTS (
    SELECT 1 FROM user_tenant_memberships m
    WHERE m.user_id = a.id AND m.tenant_id = a.tenant_id
    -- Deliberately NO deleted_at/status/source filter: a tombstone blocks backfill.
)
ON CONFLICT DO NOTHING;

-- Supply all DDL from both retired versions for direct upgrades as well as
-- installations which already applied either/both old versions.
ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS requester_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS assignee_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS created_by_membership_id BIGINT;
ALTER TABLE problems
    ADD COLUMN IF NOT EXISTS created_by_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS assignee_membership_id BIGINT;
ALTER TABLE changes
    ADD COLUMN IF NOT EXISTS created_by_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS assignee_membership_id BIGINT;
ALTER TABLE service_requests
    ADD COLUMN IF NOT EXISTS requester_membership_id BIGINT,
    ADD COLUMN IF NOT EXISTS processor_membership_id BIGINT;

-- Preserve non-NULL historical references. A suspended membership can identify a
-- historical actor; linking it does not grant access. Deleted rows are not linked.
DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('tickets', 'requester_membership_id', 'requester_id'),
            ('tickets', 'assignee_membership_id', 'assignee_id'),
            ('problems', 'created_by_membership_id', 'created_by'),
            ('problems', 'assignee_membership_id', 'assignee_id'),
            ('changes', 'created_by_membership_id', 'created_by'),
            ('changes', 'assignee_membership_id', 'assignee_id'),
            ('service_requests', 'requester_membership_id', 'requester_id'),
            ('service_requests', 'processor_membership_id', 'processor_id')
        ) AS v(tbl, colname, user_col)
    LOOP
        EXECUTE format(
            'UPDATE %I x SET %I = m.id FROM user_tenant_memberships m ' ||
            'WHERE x.%I IS NULL AND x.%I > 0 ' ||
            'AND m.user_id = x.%I AND m.tenant_id = x.tenant_id AND m.deleted_at IS NULL',
            r.tbl, r.colname, r.colname, r.user_col, r.user_col
        );
    END LOOP;
END $$;

-- Legacy tickets have no separate creator column.
UPDATE tickets t
SET created_by_membership_id = t.requester_membership_id
WHERE t.created_by_membership_id IS NULL
  AND EXISTS (
      SELECT 1 FROM user_tenant_memberships m
      WHERE m.id = t.requester_membership_id AND m.tenant_id = t.tenant_id
  );

CREATE INDEX IF NOT EXISTS idx_tickets_requester_membership
    ON tickets (tenant_id, requester_membership_id);
CREATE INDEX IF NOT EXISTS idx_tickets_assignee_membership
    ON tickets (tenant_id, assignee_membership_id);
CREATE INDEX IF NOT EXISTS idx_tickets_created_by_membership
    ON tickets (tenant_id, created_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_problems_created_by_membership
    ON problems (tenant_id, created_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_problems_assignee_membership
    ON problems (tenant_id, assignee_membership_id);
CREATE INDEX IF NOT EXISTS idx_changes_created_by_membership
    ON changes (tenant_id, created_by_membership_id);
CREATE INDEX IF NOT EXISTS idx_changes_assignee_membership
    ON changes (tenant_id, assignee_membership_id);
CREATE INDEX IF NOT EXISTS idx_service_requests_requester_membership
    ON service_requests (tenant_id, requester_membership_id);
CREATE INDEX IF NOT EXISTS idx_service_requests_processor_membership
    ON service_requests (tenant_id, processor_membership_id);

DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('tickets', 'fk_tickets_requester_membership_tenant', 'requester_membership_id'),
            ('tickets', 'fk_tickets_assignee_membership_tenant', 'assignee_membership_id'),
            ('tickets', 'fk_tickets_created_by_membership_tenant', 'created_by_membership_id'),
            ('problems', 'fk_problems_created_by_membership_tenant', 'created_by_membership_id'),
            ('problems', 'fk_problems_assignee_membership_tenant', 'assignee_membership_id'),
            ('changes', 'fk_changes_created_by_membership_tenant', 'created_by_membership_id'),
            ('changes', 'fk_changes_assignee_membership_tenant', 'assignee_membership_id'),
            ('service_requests', 'fk_service_requests_requester_membership_tenant', 'requester_membership_id'),
            ('service_requests', 'fk_service_requests_processor_membership_tenant', 'processor_membership_id')
        ) AS v(tbl, conname, colname)
    LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conname = r.conname AND conrelid = to_regclass(r.tbl)
        ) THEN
            EXECUTE format(
                'ALTER TABLE %I ADD CONSTRAINT %I FOREIGN KEY (%I, tenant_id) REFERENCES user_tenant_memberships (id, tenant_id) NOT VALID',
                r.tbl, r.conname, r.colname
            );
        END IF;
    END LOOP;
END $$;
