package multi_tenant

import (
	"testing"

	"github.com/stretchr/testify/require"

	"itsm-backend/ent/migrate"
)

// TestTenantScopedTablesExposeTenantID 锁定 RLS 矩阵的结构前提：
// 17 张核心租户表必须都带 tenant_id 列，否则应用层/数据库层都无法做租户隔离。
func TestTenantScopedTablesExposeTenantID(t *testing.T) {
	coreTables := []string{
		"tickets",
		"incidents",
		"changes",
		"problems",
		"service_requests",
		"configuration_items",
		"ci_relationships",
		"sla_definitions",
		"sla_alert_rules",
		"knowledge_articles",
		"notifications",
		"approval_chains",
		"assets",
		"releases",
		"service_catalogs",
		"groups",
		"users",
	}

	byName := make(map[string][]string, len(migrate.Tables))
	for _, table := range migrate.Tables {
		columns := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			columns = append(columns, column.Name)
		}
		byName[table.Name] = columns
	}

	for _, name := range coreTables {
		columns, ok := byName[name]
		require.True(t, ok, "table %s must be registered in ent migrate schema", name)
		require.Contains(t, columns, "tenant_id", "table %s must expose tenant_id", name)
	}
}

// TestBusinessActorMembershipColumns 锁定 Phase 4 第二批的列契约：
// 问题/变更/服务请求必须同时保留 user_id 兼容列与可空 membership 列。
func TestBusinessActorMembershipColumns(t *testing.T) {
	expectations := map[string][]string{
		"problems":         {"created_by", "created_by_membership_id", "assignee_id", "assignee_membership_id"},
		"changes":          {"created_by", "created_by_membership_id", "assignee_id", "assignee_membership_id"},
		"service_requests": {"requester_id", "requester_membership_id", "processor_id", "processor_membership_id"},
	}

	byName := make(map[string][]string, len(migrate.Tables))
	for _, table := range migrate.Tables {
		columns := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			columns = append(columns, column.Name)
		}
		byName[table.Name] = columns
	}

	for table, expected := range expectations {
		columns, ok := byName[table]
		require.True(t, ok, "table %s must be registered in ent migrate schema", table)
		for _, column := range expected {
			require.Contains(t, columns, column, "table %s must keep column %s", table, column)
		}
	}
}
