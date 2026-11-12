//go:build integration_rls

package rls

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type membershipConstraint struct {
	table      string
	constraint string
	column     string
}

// membershipConstraints 是 C 方案 Phase 2–4 的全部 membership 复合外键。
// 新增 membership 引用时必须同步此清单，否则发布巡检会漏项。
var membershipConstraints = []membershipConstraint{
	{"tickets", "fk_tickets_requester_membership_tenant", "requester_membership_id"},
	{"tickets", "fk_tickets_assignee_membership_tenant", "assignee_membership_id"},
	{"tickets", "fk_tickets_created_by_membership_tenant", "created_by_membership_id"},
	{"ticket_comments", "fk_ticket_comments_user_membership_tenant", "user_membership_id"},
	{"ticket_attachments", "fk_ticket_attachments_uploader_membership_tenant", "uploaded_by_membership_id"},
	{"attachments", "fk_attachments_uploader_membership_tenant", "uploaded_by_membership_id"},
	{"ticket_ccs", "fk_ticket_ccs_user_membership_tenant", "user_membership_id"},
	{"ticket_ccs", "fk_ticket_ccs_added_by_membership_tenant", "added_by_membership_id"},
	{"ticket_approvals", "fk_ticket_approvals_approver_membership_tenant", "approver_membership_id"},
	{"ticket_approvals", "fk_ticket_approvals_delegate_membership_tenant", "delegate_to_membership_id"},
	{"approval_records", "fk_approval_records_approver_membership_tenant", "approver_membership_id"},
	{"notifications", "fk_notifications_user_membership_tenant", "user_membership_id"},
	{"ticket_notifications", "fk_ticket_notifications_user_membership_tenant", "user_membership_id"},
	{"process_approval_decisions", "fk_process_approval_decisions_actor_membership_tenant", "actor_membership_id"},
	{"audit_logs", "fk_audit_logs_membership_tenant", "membership_id"},
	{"problems", "fk_problems_created_by_membership_tenant", "created_by_membership_id"},
	{"problems", "fk_problems_assignee_membership_tenant", "assignee_membership_id"},
	{"changes", "fk_changes_created_by_membership_tenant", "created_by_membership_id"},
	{"changes", "fk_changes_assignee_membership_tenant", "assignee_membership_id"},
	{"service_requests", "fk_service_requests_requester_membership_tenant", "requester_membership_id"},
	{"service_requests", "fk_service_requests_processor_membership_tenant", "processor_membership_id"},
}

// TestMembershipCompositeForeignKeysValidated 锁定 Phase 4 的约束收敛结果：
// 所有 membership 复合外键存在、已 VALIDATE，且没有跨租户脏引用。
func TestMembershipCompositeForeignKeysValidated(t *testing.T) {
	db := openOwnerDB(t)
	defer db.Close()
	ctx := context.Background()

	for _, fk := range membershipConstraints {
		fk := fk
		t.Run(fk.table+"."+fk.column, func(t *testing.T) {
			var validated bool
			err := db.QueryRowContext(ctx,
				`SELECT convalidated FROM pg_constraint WHERE conname = $1`,
				fk.constraint,
			).Scan(&validated)
			require.NoError(t, err, "constraint %s must exist", fk.constraint)
			require.True(t, validated, "constraint %s must be VALIDATED", fk.constraint)

			invalid := countInvalidMembershipRefs(t, db, fk.table, fk.column)
			require.Zero(t, invalid,
				"%s.%s contains %d membership reference(s) outside the row tenant",
				fk.table, fk.column, invalid)
		})
	}
}

func countInvalidMembershipRefs(t *testing.T, db *sql.DB, table, column string) int {
	t.Helper()
	query := fmt.Sprintf(
		`SELECT count(*) FROM %s t `+
			`WHERE t.%s IS NOT NULL AND NOT EXISTS (`+
			`  SELECT 1 FROM user_tenant_memberships m `+
			`  WHERE m.id = t.%s AND m.tenant_id = t.tenant_id)`,
		table, column, column,
	)
	var n int
	require.NoError(t, db.QueryRow(query).Scan(&n))
	return n
}
