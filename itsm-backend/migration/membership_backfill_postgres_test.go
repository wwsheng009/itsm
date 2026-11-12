//go:build integration_migration

package migration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Opt-in only: no application config, default connection, or production database.
// Example (against a disposable, loopback PostgreSQL database ending in _test):
//
//	ITSM_MEMBERSHIP_MIGRATION_TEST_DSN='postgres://test:test@127.0.0.1:55433/itsm_membership_test?sslmode=disable' \
//	go test -tags integration_migration -p 1 ./migration -run '^TestMembershipBackfill' -count=1 -v
//
// Every case owns a random schema, uses a search_path WITHOUT public, and drops
// only that schema. The migrations are executed unchanged by the real Migrator.
func openMembershipMigrationTestDB(t *testing.T) (*sql.DB, func() []string) {
	t.Helper()
	raw := os.Getenv("ITSM_MEMBERSHIP_MIGRATION_TEST_DSN")
	if raw == "" {
		t.Skip("set ITSM_MEMBERSHIP_MIGRATION_TEST_DSN to a disposable local PostgreSQL *_test database")
	}
	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, u.Scheme)
	require.Contains(t, []string{"127.0.0.1", "localhost", "::1"}, u.Hostname(), "tests must use loopback PostgreSQL")
	require.True(t, strings.HasSuffix(u.Path, "_test"), "refusing a database not explicitly named *_test")
	// Disallow query parameters that can override the validated host/database.
	for key := range u.Query() {
		require.Contains(t, []string{"sslmode", "connect_timeout"}, key, "unsupported test DSN option")
	}
	schema := "membership_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	params := u.Query()
	params.Set("search_path", schema)
	params.Set("connect_timeout", "5")
	params.Set("statement_timeout", "10000")
	params.Set("lock_timeout", "3000")
	u.RawQuery = params.Encode()
	connector, err := pq.NewConnector(u.String())
	require.NoError(t, err)
	var mu sync.Mutex
	var warnings []string
	db := sql.OpenDB(pq.ConnectorWithNoticeHandler(connector, func(notice *pq.Error) {
		if notice.Severity == "WARNING" {
			mu.Lock()
			warnings = append(warnings, notice.Message)
			mu.Unlock()
		}
	}))
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(schema)+" CASCADE")
		if err != nil {
			t.Errorf("drop isolated test schema: %v", err)
		}
		_ = db.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	return db, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), warnings...)
	}
}

type membershipActorFixture struct {
	table   string
	users   []string
	columns []string
}

var membershipActorFixtures = []membershipActorFixture{
	{"tickets", []string{"requester_id", "assignee_id"}, []string{"requester_membership_id", "assignee_membership_id", "created_by_membership_id"}},
	{"problems", []string{"created_by", "assignee_id"}, []string{"created_by_membership_id", "assignee_membership_id"}},
	{"changes", []string{"created_by", "assignee_id"}, []string{"created_by_membership_id", "assignee_membership_id"}},
	{"service_requests", []string{"requester_id", "processor_id"}, []string{"requester_membership_id", "processor_membership_id"}},
	{"ticket_comments", []string{"user_id"}, []string{"user_membership_id"}},
	{"ticket_attachments", []string{"uploaded_by"}, []string{"uploaded_by_membership_id"}},
	{"attachments", []string{"uploaded_by"}, []string{"uploaded_by_membership_id"}},
	{"ticket_ccs", []string{"user_id", "added_by"}, []string{"user_membership_id", "added_by_membership_id"}},
	{"ticket_approvals", []string{"approver_id", "delegate_to_user_id"}, []string{"approver_membership_id", "delegate_to_membership_id"}},
	{"approval_records", []string{"approver_id"}, []string{"approver_membership_id"}},
	{"notifications", []string{"user_id"}, []string{"user_membership_id"}},
	{"ticket_notifications", []string{"user_id"}, []string{"user_membership_id"}},
	{"process_approval_decisions", []string{"actor_id"}, []string{"actor_membership_id"}},
	{"service_request_approvals", []string{"approver_id", "delegated_to_id"}, []string{"approver_membership_id", "delegated_to_membership_id"}},
}

func membershipExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), query, args...)
	require.NoError(t, err)
}

func createMembershipActorFixture(t *testing.T, db *sql.DB, fixture membershipActorFixture) {
	t.Helper()
	definition := "id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL"
	for _, col := range fixture.users {
		definition += ", " + pq.QuoteIdentifier(col) + " BIGINT"
	}
	membershipExec(t, db, "CREATE TABLE "+pq.QuoteIdentifier(fixture.table)+" ("+definition+")")
	// A/B tenants, revoked identity, suspended identity, and an identity that
	// only appears AFTER old migrations in the upgrade test.
	for _, row := range [][3]int{{1, 10, 1}, {2, 20, 1}, {3, 10, 10}, {4, 10, 13}, {5, 10, 30}} {
		values := fmt.Sprintf("%d, %d", row[0], row[1])
		for range fixture.users {
			values += fmt.Sprintf(", %d", row[2])
		}
		membershipExec(t, db, "INSERT INTO "+pq.QuoteIdentifier(fixture.table)+" VALUES ("+values+")")
	}
}

func seedMembershipMigrationFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	// Minimal pre-November DDL with the actual partial unique index semantics.
	// Avoid Ent Schema.Create: it already includes the new columns and would
	// hide a missing replacement dependency on a real direct upgrade.
	membershipExec(t, db, `
		CREATE TABLE tenants (id BIGINT PRIMARY KEY, type TEXT NOT NULL);
		CREATE TABLE users (
			id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL REFERENCES tenants(id),
			role TEXT NOT NULL, msp_role TEXT, is_bootstrap_admin BOOLEAN NOT NULL DEFAULT false
		);
		CREATE TABLE roles (
			id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL REFERENCES tenants(id),
			code TEXT NOT NULL, UNIQUE (tenant_id, code)
		);
		CREATE TABLE user_tenant_memberships (
			id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id),
			tenant_id BIGINT NOT NULL REFERENCES tenants(id), account_kind TEXT NOT NULL,
			subject_type TEXT NOT NULL DEFAULT 'user', source TEXT NOT NULL,
			role_id BIGINT REFERENCES roles(id), msp_role TEXT,
			status TEXT NOT NULL DEFAULT 'active', is_default BOOLEAN NOT NULL DEFAULT false,
			deleted_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE UNIQUE INDEX uq_membership_live ON user_tenant_memberships(user_id, tenant_id) WHERE deleted_at IS NULL;
		CREATE UNIQUE INDEX uq_membership_default ON user_tenant_memberships(user_id) WHERE deleted_at IS NULL AND is_default AND status = 'active';
		CREATE UNIQUE INDEX uq_customer_single_scope ON user_tenant_memberships(user_id) WHERE deleted_at IS NULL AND status = 'active' AND account_kind = 'customer';
		CREATE UNIQUE INDEX uq_membership_id_tenant ON user_tenant_memberships(id, tenant_id);
		CREATE TABLE audit_logs (id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL, membership_id BIGINT);
		INSERT INTO tenants VALUES (10, 'msp_provider'), (20, 'msp_provider'), (30, 'saas_customer'), (40, 'platform'), (50, 'msp_provider');
		INSERT INTO roles VALUES
			(1, 20, 'msp_tech'), (101, 10, 'admin'), (102, 10, 'msp_tech'),
			(103, 10, 'msp_manager'), (104, 10, 'provider_tenant_admin'),
			(105, 30, 'end_user'), (106, 40, 'admin'), (109, 50, 'admin');
		INSERT INTO users VALUES
			(1, 10, 'admin', 'provider_agent', false),
			(2, 10, 'admin', 'provider_admin', true),
			(3, 10, 'admin', 'provider_admin', false),
			(4, 30, 'end_user', 'customer_user', false),
			(5, 10, ' ADMIN ', '', false),
			(6, 40, 'admin', NULL, false),
			(7, 50, 'admin', 'provider_agent', false),
			(9, 10, 'admin', ' Provider_Agent ', false),
			(10, 10, 'admin', 'provider_agent', false),
			(11, 10, 'admin', 'provider_agent', false),
			(12, 10, 'admin', 'provider_agent', false),
			(13, 10, 'admin', 'provider_agent', false),
			(14, 10, 'admin', 'provider_agent', false),
			(15, 10, 'admin', 'provider_agent', false),
			(16, 10, 'admin', 'provider_agent', false);
		INSERT INTO user_tenant_memberships
			(id, user_id, tenant_id, account_kind, source, role_id, msp_role, status, is_default, deleted_at) VALUES
			(1000, 10, 10, 'provider', 'invite', 102, 'msp_tech', 'suspended', false, now()),
			(1001, 11, 10, 'provider', 'home', 102, 'msp_tech', 'suspended', false, now()),
			(1002, 12, 10, 'provider', 'migration', 102, 'msp_tech', 'suspended', false, now()),
			(1003, 13, 10, 'provider', 'invite', 101, NULL, 'suspended', true, NULL),
			(1004, 9, 20, 'provider', 'invite', 1, 'msp_tech', 'suspended', false, now()),
			(1005, 14, 10, 'provider', 'home', 101, NULL, 'active', true, NULL),
			(1006, 15, 10, 'provider', 'invite', 102, 'msp_tech', 'suspended', false, now()),
			(1007, 15, 10, 'provider', 'migration', 102, 'msp_tech', 'active', true, NULL),
			(1008, 16, 20, 'provider', 'invite', 1, 'msp_tech', 'active', true, NULL);
	`)
	for _, fixture := range membershipActorFixtures {
		createMembershipActorFixture(t, db, fixture)
	}
	require.NoError(t, NewMigrator(db, zap.NewNop().Sugar()).EnsureMigrationsTable(context.Background()))
}

func membershipSnapshot(t *testing.T, db *sql.DB, predicate string) string {
	t.Helper()
	var snapshot string
	require.NoError(t, db.QueryRow(`SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY m.id), '[]'::jsonb)::text
		FROM user_tenant_memberships m WHERE `+predicate).Scan(&snapshot))
	return snapshot
}

func assertMembershipActorReferences(t *testing.T, db *sql.DB, rowID int, want sql.NullInt64) {
	t.Helper()
	for _, fixture := range membershipActorFixtures {
		for _, column := range fixture.columns {
			var got sql.NullInt64
			query := fmt.Sprintf("SELECT %s FROM %s WHERE id = $1", pq.QuoteIdentifier(column), pq.QuoteIdentifier(fixture.table))
			require.NoError(t, db.QueryRow(query, rowID).Scan(&got))
			require.Equal(t, want, got, "%s.%s row=%d", fixture.table, column, rowID)
		}
	}
}

func membershipHomeID(t *testing.T, db *sql.DB, userID int) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRow(`SELECT m.id FROM user_tenant_memberships m JOIN users u ON u.id = m.user_id
		WHERE u.id = $1 AND m.tenant_id = u.tenant_id AND m.deleted_at IS NULL`, userID).Scan(&id))
	return id
}

func TestMembershipBackfillPostgres(t *testing.T) {
	t.Run("direct_upgrade", func(t *testing.T) {
		db, warnings := openMembershipMigrationTestDB(t)
		seedMembershipMigrationFixture(t, db)
		before := membershipSnapshot(t, db, "id >= 1000")
		m := NewMigrator(db, zap.NewNop().Sugar())
		stream := membershipMigrationStream(t)
		count, err := m.RunMigrations(context.Background(), stream)
		require.NoError(t, err)
		require.Equal(t, len(stream)-2, count, "both unsafe versions must be adopted, never executed")
		require.Equal(t, before, membershipSnapshot(t, db, "id >= 1000"), "existing grants and tombstones are immutable")
		for _, userID := range []int{10, 11, 12} {
			var total, live int
			require.NoError(t, db.QueryRow(`SELECT count(*), count(*) FILTER (WHERE deleted_at IS NULL)
				FROM user_tenant_memberships WHERE user_id = $1 AND tenant_id = 10`, userID).Scan(&total, &live))
			require.Equal(t, 1, total, "revoked user=%d must not gain a second row", userID)
			require.Zero(t, live)
		}
		for _, tc := range []struct {
			userID  int
			roleID  int64
			mspRole string
			kind    string
		}{
			{1, 102, "msp_tech", "provider"},
			{2, 104, "msp_admin", "provider"},
			{3, 103, "msp_manager", "provider"},
			{4, 105, "", "customer"},
			{5, 101, "", "provider"},
			{6, 106, "", "platform"},
			{7, 0, "msp_tech", "provider"},   // no tenant-local msp_tech; never substitute admin
			{9, 102, "msp_tech", "provider"}, // another tenant's tombstone must not block home
		} {
			var role sql.NullInt64
			var mspRole sql.NullString
			var kind, source string
			require.NoError(t, db.QueryRow(`SELECT role_id, msp_role, account_kind, source FROM user_tenant_memberships WHERE id = $1`,
				membershipHomeID(t, db, tc.userID)).Scan(&role, &mspRole, &kind, &source))
			require.Equal(t, sql.NullInt64{Int64: tc.roleID, Valid: tc.roleID != 0}, role, "user=%d", tc.userID)
			require.Equal(t, sql.NullString{String: tc.mspRole, Valid: tc.mspRole != ""}, mspRole, "user=%d", tc.userID)
			require.Equal(t, tc.kind, kind)
			require.Equal(t, "migration", source)
		}
		var conflictedHome int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_tenant_memberships WHERE user_id = 16 AND tenant_id = 10`).Scan(&conflictedHome))
		require.Zero(t, conflictedHome, "do not displace an existing default in another tenant")
		assertMembershipActorReferences(t, db, 1, sql.NullInt64{Int64: membershipHomeID(t, db, 1), Valid: true})
		assertMembershipActorReferences(t, db, 2, sql.NullInt64{})                         // wrong tenant
		assertMembershipActorReferences(t, db, 3, sql.NullInt64{})                         // revoked invitation
		assertMembershipActorReferences(t, db, 4, sql.NullInt64{Int64: 1003, Valid: true}) // historical, not reactivated
		assertMembershipActorReferences(t, db, 5, sql.NullInt64{})                         // no identity
		var validated int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM pg_constraint
			WHERE connamespace = current_schema()::regnamespace AND conname LIKE 'fk_%membership%tenant' AND convalidated`).Scan(&validated))
		require.Equal(t, 23, validated, "replacement DDL must precede both constraint validation migrations")
		for _, version := range []string{"20261111_membership_tenant_identity", "20261112_membership_business_actors"} {
			var checksum, release string
			require.NoError(t, db.QueryRow(`SELECT checksum, release_version FROM schema_migrations WHERE version = $1`, version).Scan(&checksum, &release))
			require.Empty(t, checksum)
			require.Equal(t, "superseded", release)
		}
		require.Contains(t, strings.Join(warnings(), "\n"), "historical_pairs=1, legacy_msp_roles=0, missing_roles=1")
		snapshot := membershipSnapshot(t, db, "true")
		count, err = m.RunMigrations(context.Background(), stream)
		require.NoError(t, err)
		require.Zero(t, count)
		require.Equal(t, snapshot, membershipSnapshot(t, db, "true"))

		// Simulate a later revocation and a lost NEW migration ledger entry. Even
		// re-running the safe SQL must never resurrect a previously backfilled row.
		membershipExec(t, db, `UPDATE user_tenant_memberships SET deleted_at = now(), status = 'suspended' WHERE user_id = 1`)
		membershipExec(t, db, `DELETE FROM schema_migrations WHERE version IN ($1, $2)`, membershipSafeVersion, membershipReconcileVersion)
		snapshot = membershipSnapshot(t, db, "true")
		count, err = m.RunMigrations(context.Background(), stream)
		require.NoError(t, err)
		require.Equal(t, 2, count)
		require.Equal(t, snapshot, membershipSnapshot(t, db, "true"), "replay after revocation must be inert")
	})
}

func TestMembershipBackfillPostgresPreviouslyApplied(t *testing.T) {
	for _, full := range []bool{false, true} {
		name := "tenant_identity_already_applied"
		if full {
			name = "entire_old_stream_already_applied"
		}
		t.Run(name, func(t *testing.T) {
			db, warnings := openMembershipMigrationTestDB(t)
			seedMembershipMigrationFixture(t, db)
			m := NewMigrator(db, zap.NewNop().Sugar())
			stream := membershipMigrationStream(t)
			oldLedger := map[string]string{}
			for _, mig := range stream {
				if mig.Version == membershipSafeVersion || mig.Version == membershipReconcileVersion {
					continue
				}
				if !full && mig.Version > "20261111_membership_tenant_identity" {
					continue
				}
				// Reproduce the old release, including its unsafe data, only inside
				// this isolated test schema. Do not alter the historical SQL bytes.
				require.NoError(t, m.ApplyMigration(context.Background(), mig))
				var row string
				require.NoError(t, db.QueryRow(`SELECT to_jsonb(l)::text FROM schema_migrations l WHERE version = $1`, mig.Version).Scan(&row))
				oldLedger[mig.Version] = row
			}
			var missed sql.NullInt64
			require.NoError(t, db.QueryRow(`SELECT user_membership_id FROM ticket_comments WHERE id = 1`).Scan(&missed))
			require.False(t, missed.Valid, "prove the published ordering bug before the fix")
			var resurrected int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_tenant_memberships
				WHERE user_id = 10 AND tenant_id = 10 AND deleted_at IS NULL AND source = 'migration'`).Scan(&resurrected))
			require.Equal(t, 1, resurrected, "prove the old revoked-invite bug before the fix")
			var oldRole string
			require.NoError(t, db.QueryRow(`SELECT msp_role FROM user_tenant_memberships WHERE user_id = 1 AND tenant_id = 10`).Scan(&oldRole))
			require.Equal(t, "provider_agent", oldRole, "prove the old vocabulary bug before the fix")
			before := membershipSnapshot(t, db, "user_id <> 30")
			// A legitimate existing non-NULL historical pointer must not be
			// overwritten, even when user_id now resolves to another membership.
			membershipExec(t, db, `UPDATE ticket_comments SET user_membership_id = 1005 WHERE id = 4`)
			membershipExec(t, db, `INSERT INTO users VALUES (30, 10, 'admin', 'provider_admin', true)`)
			_, err := m.RunMigrations(context.Background(), stream)
			require.NoError(t, err)
			require.Equal(t, before, membershipSnapshot(t, db, "user_id <> 30"),
				"never infer revocation or overwrite authorization from source=migration")
			for version, want := range oldLedger {
				var got string
				require.NoError(t, db.QueryRow(`SELECT to_jsonb(l)::text FROM schema_migrations l WHERE version = $1`, version).Scan(&got))
				require.Equal(t, want, got, "all existing checksum/provenance fields remain intact: %s", version)
			}
			var roleID int64
			var mspRole string
			require.NoError(t, db.QueryRow(`SELECT role_id, msp_role FROM user_tenant_memberships WHERE user_id = 30 AND tenant_id = 10`).Scan(&roleID, &mspRole))
			require.EqualValues(t, 104, roleID)
			require.Equal(t, "msp_admin", mspRole)
			assertMembershipActorReferences(t, db, 1, sql.NullInt64{Int64: membershipHomeID(t, db, 1), Valid: true})
			assertMembershipActorReferences(t, db, 2, sql.NullInt64{})
			assertMembershipActorReferences(t, db, 5, sql.NullInt64{Int64: membershipHomeID(t, db, 30), Valid: true})
			var preserved int64
			require.NoError(t, db.QueryRow(`SELECT user_membership_id FROM ticket_comments WHERE id = 4`).Scan(&preserved))
			require.EqualValues(t, 1005, preserved)
			require.Contains(t, strings.Join(warnings(), "\n"), "membership backfill manual review required")
			require.Contains(t, strings.Join(warnings(), "\n"), "existing grants and roles left unchanged")
		})
	}
}

func TestMembershipBackfillPostgresRollbackAndRetry(t *testing.T) {
	db, _ := openMembershipMigrationTestDB(t)
	seedMembershipMigrationFixture(t, db)
	before := membershipSnapshot(t, db, "true")
	// Force failure AFTER identity inserts and some DDL. The runner must roll
	// back the entire safe migration and leave its version pending for retry.
	membershipExec(t, db, `DROP TABLE changes`)
	m := NewMigrator(db, zap.NewNop().Sugar())
	stream := membershipMigrationStream(t)
	_, err := m.RunMigrations(context.Background(), stream)
	require.ErrorContains(t, err, membershipSafeVersion)
	require.Equal(t, before, membershipSnapshot(t, db, "true"))
	var recorded, columns int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version = $1`, membershipSafeVersion).Scan(&recorded))
	require.Zero(t, recorded)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'tickets' AND column_name = 'requester_membership_id'`).Scan(&columns))
	require.Zero(t, columns, "the replacement's DDL must roll back with its data")
	createMembershipActorFixture(t, db, membershipActorFixtures[2])
	_, err = m.RunMigrations(context.Background(), stream)
	require.NoError(t, err, "adopted old versions must not prevent safe retry")
	assertMembershipActorReferences(t, db, 1, sql.NullInt64{Int64: membershipHomeID(t, db, 1), Valid: true})
	assertMembershipActorReferences(t, db, 3, sql.NullInt64{})
}
