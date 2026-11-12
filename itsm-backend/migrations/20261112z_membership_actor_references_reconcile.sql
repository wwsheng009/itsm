-- 20261112z_membership_actor_references_reconcile
-- Forward-only completion of the membership safety upgrade. The z suffix puts
-- this version AFTER the entire published 20261112 stream (also advancing readyz).
-- actor_references sorted before tenant_identity, so its first pass could not
-- see the memberships created later. Reconcile only NULL, tenant-local pointers
-- AFTER 20261112_membership_backfill_safe, for both direct and previously-applied upgrades.
-- No new memberships, role changes, tombstone removal, or automatic revocations.

DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT * FROM (VALUES
            ('ticket_comments', 'user_membership_id', 'user_id'),
            ('ticket_attachments', 'uploaded_by_membership_id', 'uploaded_by'),
            ('attachments', 'uploaded_by_membership_id', 'uploaded_by'),
            ('ticket_ccs', 'user_membership_id', 'user_id'),
            ('ticket_ccs', 'added_by_membership_id', 'added_by'),
            ('ticket_approvals', 'approver_membership_id', 'approver_id'),
            ('ticket_approvals', 'delegate_to_membership_id', 'delegate_to_user_id'),
            ('approval_records', 'approver_membership_id', 'approver_id'),
            ('notifications', 'user_membership_id', 'user_id'),
            ('ticket_notifications', 'user_membership_id', 'user_id'),
            ('process_approval_decisions', 'actor_membership_id', 'actor_id'),
            -- The approval migration may also have run before a missing home
            -- identity was safely repaired on an existing installation.
            ('service_request_approvals', 'approver_membership_id', 'approver_id'),
            ('service_request_approvals', 'delegated_to_membership_id', 'delegated_to_id')
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

-- Read-only review signals. These are candidates, NOT evidence that a grant was
-- unauthorized: source=migration carries no migration version/causal grant ID.
-- In particular, a tombstone plus a later live row may be a legitimate re-grant.
-- Reconcile with invitation/revocation audit evidence before any operator change.
-- Drivers may discard PostgreSQL notices; release verification must also run
-- these SELECT predicates read-only (select m.id, m.user_id, m.tenant_id for IDs).
DO $$
DECLARE
    historical_pairs bigint;
    legacy_msp_roles bigint;
    missing_roles bigint;
BEGIN
    SELECT count(*) INTO historical_pairs
    FROM user_tenant_memberships m
    WHERE m.source = 'migration' AND m.deleted_at IS NULL
      AND EXISTS (
          SELECT 1 FROM user_tenant_memberships revoked
          WHERE revoked.user_id = m.user_id AND revoked.tenant_id = m.tenant_id
            AND revoked.deleted_at IS NOT NULL
      );

    SELECT count(*) INTO legacy_msp_roles
    FROM user_tenant_memberships m
    WHERE m.source = 'migration' AND m.deleted_at IS NULL
      AND m.msp_role IN ('provider_admin', 'provider_agent', 'customer_user');

    SELECT count(*) INTO missing_roles
    FROM user_tenant_memberships m
    WHERE m.source = 'migration' AND m.deleted_at IS NULL AND m.role_id IS NULL;

    IF historical_pairs > 0 OR legacy_msp_roles > 0 OR missing_roles > 0 THEN
        RAISE WARNING 'membership backfill manual review required: historical_pairs=%, legacy_msp_roles=%, missing_roles=%; candidates only, existing grants and roles left unchanged',
            historical_pairs, legacy_msp_roles, missing_roles;
    END IF;
END $$;
