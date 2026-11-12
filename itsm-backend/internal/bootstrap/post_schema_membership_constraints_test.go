package bootstrap

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// requiredMembershipConstraints 是 C 方案 Phase 2–4 的 canonical 约束清单。
// 任何新增 membership 引用都必须同时出现在：
//   - 版本化迁移（业务表迁移 + 约束 VALIDATE 迁移）；
//   - post_schema_constraints.sql（Schema.Create 重建后的自愈路径）。
var requiredMembershipConstraints = []string{
	"fk_tickets_requester_membership_tenant",
	"fk_tickets_assignee_membership_tenant",
	"fk_tickets_created_by_membership_tenant",
	"fk_ticket_comments_user_membership_tenant",
	"fk_ticket_attachments_uploader_membership_tenant",
	"fk_attachments_uploader_membership_tenant",
	"fk_ticket_ccs_user_membership_tenant",
	"fk_ticket_ccs_added_by_membership_tenant",
	"fk_ticket_approvals_approver_membership_tenant",
	"fk_ticket_approvals_delegate_membership_tenant",
	"fk_approval_records_approver_membership_tenant",
	"fk_notifications_user_membership_tenant",
	"fk_ticket_notifications_user_membership_tenant",
	"fk_process_approval_decisions_actor_membership_tenant",
	"fk_audit_logs_membership_tenant",
	"fk_problems_created_by_membership_tenant",
	"fk_problems_assignee_membership_tenant",
	"fk_changes_created_by_membership_tenant",
	"fk_changes_assignee_membership_tenant",
	"fk_service_requests_requester_membership_tenant",
	"fk_service_requests_processor_membership_tenant",
}

func TestPostSchemaConstraintsCoverMembershipReferences(t *testing.T) {
	for _, constraint := range requiredMembershipConstraints {
		require.Contains(t, postSchemaConstraintsSQL, constraint,
			"post_schema_constraints.sql must self-heal %s", constraint)
	}
}

func TestVersionedMigrationsCoverMembershipReferences(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")

	all := make([]byte, 0, 32*1024)
	for _, file := range []string{
		"20261111_membership_tenant_identity.sql",
		"20261111_membership_actor_references.sql",
		"20261112_membership_business_actors.sql",
		"20261112_membership_constraints_validate.sql",
	} {
		body, err := os.ReadFile(filepath.Join(migrationsDir, file))
		require.NoError(t, err)
		all = append(all, body...)
	}
	for _, constraint := range requiredMembershipConstraints {
		require.Contains(t, string(all), constraint,
			"versioned migrations must define or validate %s", constraint)
	}
}
