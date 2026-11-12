package migration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	membershipSafeVersion      = "20261112_membership_backfill_safe"
	membershipReconcileVersion = "20261112z_membership_actor_references_reconcile"
)

// Use the real discovery/merge entry points, not SQL duplicated in the tests.
// The fixture starts at the pre-November membership schema; unrelated migrations
// (RBAC, CMDB, etc.) are not part of this narrowly scoped upgrade regression.
func membershipMigrationStream(t *testing.T) []Migration {
	t.Helper()
	fs, err := FilesystemMigrations("../migrations")
	require.NoError(t, err)
	wanted := map[string]bool{
		"20261111_membership_actor_references":          true,
		"20261111_membership_tenant_identity":           true,
		membershipSafeVersion:                           true,
		"20261112_membership_business_actors":           true,
		"20261112_membership_business_actors_reconcile": true,
		"20261112_membership_constraints_validate":      true,
		"20261112_service_request_approval_membership":  true,
		membershipReconcileVersion:                      true,
	}
	var stream []Migration
	for _, mig := range MergeWithRegistered(fs) {
		if wanted[mig.Version] {
			stream = append(stream, mig)
		}
	}
	require.Len(t, stream, len(wanted))
	return stream
}

func membershipMigrationByVersion(t *testing.T, version string) Migration {
	t.Helper()
	for _, mig := range membershipMigrationStream(t) {
		if mig.Version == version {
			return mig
		}
	}
	t.Fatalf("missing discovered migration %s", version)
	return Migration{}
}

func TestMembershipBackfillDiscoveryOrder(t *testing.T) {
	positions := map[string]int{}
	for i, mig := range membershipMigrationStream(t) {
		positions[mig.Version] = i
	}
	require.Less(t, positions["20261111_membership_actor_references"], positions[membershipSafeVersion])
	require.Less(t, positions[membershipSafeVersion], positions["20261112_membership_business_actors_reconcile"])
	require.Less(t, positions[membershipSafeVersion], positions["20261112_membership_constraints_validate"])
	require.Less(t, positions["20261112_service_request_approval_membership"], positions[membershipReconcileVersion])
	require.Equal(t, 7, positions[membershipReconcileVersion], "completion marker must follow the old stream")
	for _, old := range []string{"20261111_membership_tenant_identity", "20261112_membership_business_actors"} {
		require.Equal(t, membershipSafeVersion, supersededMigrations[old].Replacement)
	}
}

func TestMembershipBackfillPublishedSQLUnchanged(t *testing.T) {
	// Pin the published LF bytes; Windows checkout CRLF is normalized ONLY in
	// this test. The production runner still hashes raw SQL without normalization.
	checksums := map[string]string{
		"20261111_membership_actor_references": "0d1dc33ad6ab9a5bbe3abc49a7a74dccaaae4c05747593ba712a67b238102179",
		"20261111_membership_tenant_identity":  "001f0287e344cc69ff7e0513bffd4c29095143f83a1d38b8ddd5a90f785620fd",
		"20261112_membership_business_actors":  "07500ff4990eb09c9163dc42046918122eee43db9dff8810e556d021e2e38f23",
	}
	for version, want := range checksums {
		sql := membershipMigrationByVersion(t, version).SQLContent
		require.Equal(t, want, checksumSQL(strings.ReplaceAll(sql, "\r\n", "\n")), version)
	}
}

func TestMembershipBackfillSupersessionRequiresReplacement(t *testing.T) {
	m := NewMigrator(nil, zap.NewNop().Sugar())
	require.NoError(t, m.reconcileSupersededMigrations(context.Background(), []Migration{
		{Version: "20261111_membership_tenant_identity"},
		{Version: "20261112_membership_business_actors"},
	}))
}

func TestMembershipBackfillSupersessionPreservesChecksumValidation(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		name := "published_checksums"
		if tamper {
			name = "tampering_is_still_rejected"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			defer db.Close()
			mock.MatchExpectationsInOrder(false) // superseded registry is a map
			stream := membershipMigrationStream(t)
			rows := sqlmock.NewRows([]string{
				"version", "description", "applied_at", "rollback_sql", "checksum", "execution_ms", "release_version",
			})
			for _, version := range []string{"20261111_membership_tenant_identity", "20261112_membership_business_actors"} {
				old := membershipMigrationByVersion(t, version)
				mock.ExpectQuery(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`).
					WithArgs(version).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				rows.AddRow(version, old.Description, time.Now(), old.RollbackSQL, checksumSQL(old.SQLContent), 1, "old-release")
			}
			mock.ExpectQuery(`SELECT version, description, applied_at, rollback_sql, checksum, execution_ms, release_version
				FROM schema_migrations ORDER BY version`).WillReturnRows(rows)
			m := NewMigrator(db, zap.NewNop().Sugar())
			if tamper {
				for i := range stream {
					if stream[i].Version == "20261111_membership_tenant_identity" {
						stream[i].SQLContent += "\n-- tampered"
					}
				}
				_, err = m.RunMigrations(context.Background(), stream)
				require.ErrorContains(t, err, "migration checksum mismatch for 20261111_membership_tenant_identity")
			} else {
				require.NoError(t, m.reconcileSupersededMigrations(context.Background(), stream))
				pending, err := m.GetPendingMigrations(context.Background(), stream)
				require.NoError(t, err)
				require.Len(t, pending, len(stream)-2)
			}
			// No INSERT/UPDATE of an existing ledger entry is expected or allowed.
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
