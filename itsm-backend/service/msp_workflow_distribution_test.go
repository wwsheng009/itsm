package service

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 跨租户单据流转 P2-①：模板分发服务测试（sqlmock；授权入口注入替身，审计走 nil client 跳过）。

type stubCustomerAccessChecker struct{ err error }

func (s stubCustomerAccessChecker) CanAccessCustomer(context.Context, int, int) error { return s.err }

func newDistributionServiceForTest(t *testing.T, guard customerAccessChecker) (*MSPWorkflowDistributionService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc := NewMSPWorkflowDistributionService(db, NewBPMNWorkflowTemplateCatalog(db), guard, nil, nil)
	return svc, mock
}

func distributionActor() MSPWorkbenchActor {
	return MSPWorkbenchActor{UserID: 7, Username: "msp-admin", HomeTenantID: 3}
}

var providerTemplateSelectRe = regexp.QuoteMeta(
	"SELECT key,name,description,domain,form_schema,approval_policy,ontology_bindings,sla_config,bpmn_xml,version FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND status='published'")

func expectProviderTemplate(mock sqlmock.Sqlmock, key, version string) {
	mock.ExpectQuery(providerTemplateSelectRe).
		WithArgs(3, key).
		WillReturnRows(sqlmock.NewRows([]string{"key", "name", "description", "domain", "form_schema", "approval_policy", "ontology_bindings", "sla_config", "bpmn_xml", "version"}).
			AddRow(key, "通用工单流程", "", "it", []byte(`{}`), []byte(`{}`), []byte(`{}`), []byte(`{}`), []byte(`"<xml/>"`), version))
}

func TestDistributeWorkflowTemplate_Success(t *testing.T) {
	svc, mock := newDistributionServiceForTest(t, stubCustomerAccessChecker{})
	ctx := context.Background()

	expectProviderTemplate(mock, "ticket_general_flow", "1.2.0")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND status='draft')")).
		WithArgs(6, "ticket_general_flow").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT version FROM workflow_templates WHERE tenant_id=$1 AND key=$2 ORDER BY id DESC LIMIT 1")).
		WithArgs(6, "ticket_general_flow").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("1.0.0"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO workflow_templates")).
		WithArgs("ticket_general_flow", "通用工单流程", "", "it", "{}", "{}", "{}", "{}", `"<xml/>"`, "1.1.0", 6, 7).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(55))
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO workflow_template_distributions")).
		WithArgs(6, 3, "ticket_general_flow", "1.2.0", "ticket_general_flow", "1.1.0", 7).
		WillReturnRows(sqlmock.NewRows([]string{"id", "distributed_at"}).AddRow(12, now))

	resp, err := svc.Distribute(ctx, distributionActor(), 6, "ticket_general_flow", "")
	require.NoError(t, err)
	require.NotNil(t, resp.Distribution)
	assert.Equal(t, 12, resp.Distribution.ID)
	assert.Equal(t, 6, resp.Distribution.CustomerTenantID)
	assert.Equal(t, "1.2.0", resp.Distribution.SourceVersion)
	assert.Equal(t, "1.1.0", resp.Distribution.CustomerTemplateVersion)
	assert.Equal(t, "draft", resp.Distribution.CustomerTemplateStatus)
	assert.False(t, resp.Distribution.UpgradeAvailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributeWorkflowTemplate_AccessDenied(t *testing.T) {
	guardErr := NewCustomerAccessError(CodeMSPAllocationRequired, "目标客户未分配或不可访问")
	svc, mock := newDistributionServiceForTest(t, stubCustomerAccessChecker{err: guardErr})

	_, err := svc.Distribute(context.Background(), distributionActor(), 6, "ticket_general_flow", "")
	require.Error(t, err)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributeWorkflowTemplate_ProviderTemplateNotFound(t *testing.T) {
	svc, mock := newDistributionServiceForTest(t, stubCustomerAccessChecker{})
	mock.ExpectQuery(providerTemplateSelectRe).
		WithArgs(3, "missing_flow").
		WillReturnRows(sqlmock.NewRows([]string{"key"}))

	_, err := svc.Distribute(context.Background(), distributionActor(), 6, "missing_flow", "")
	assert.True(t, errors.Is(err, ErrWorkflowTemplateNotFound), "want ErrWorkflowTemplateNotFound, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributeWorkflowTemplate_CustomerDraftConflict(t *testing.T) {
	svc, mock := newDistributionServiceForTest(t, stubCustomerAccessChecker{})
	expectProviderTemplate(mock, "ticket_general_flow", "1.2.0")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND status='draft')")).
		WithArgs(6, "ticket_general_flow").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	_, err := svc.Distribute(context.Background(), distributionActor(), 6, "ticket_general_flow", "")
	assert.True(t, errors.Is(err, ErrWorkflowTemplateConflict), "want ErrWorkflowTemplateConflict, got %v", err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListDistributions_UpgradeHint(t *testing.T) {
	svc, mock := newDistributionServiceForTest(t, stubCustomerAccessChecker{})
	ctx := context.Background()
	now := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT "+workflowTemplateDistributionColumns+" FROM workflow_template_distributions WHERE tenant_id=$1 AND provider_tenant_id=$2 ORDER BY distributed_at DESC, id DESC")).
		WithArgs(6, 3).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "provider_tenant_id", "source_template_key", "source_version", "customer_template_key", "customer_template_version", "distributed_by", "distributed_at"}).
			AddRow(12, 6, 3, "ticket_general_flow", "1.1.0", "ticket_general_flow", "1.2.0", 7, now).
			AddRow(11, 6, 3, "ticket_general_flow", "1.2.0", "ticket_general_flow", "1.3.0", 7, now.Add(-time.Hour)))
	// 第 1 行：副本状态 + provider 最新版本（1.2.0）→ 可升级。
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND version=$3")).
		WithArgs(6, "ticket_general_flow", "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("published"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT version FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND status='published' ORDER BY updated_at DESC, id DESC LIMIT 1")).
		WithArgs(3, "ticket_general_flow").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("1.2.0"))
	// 第 2 行：副本已归档；最新版本走缓存（无第二次查询）→ 来源=最新 → 不可升级。
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status FROM workflow_templates WHERE tenant_id=$1 AND key=$2 AND version=$3")).
		WithArgs(6, "ticket_general_flow", "1.3.0").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("archived"))

	resp, err := svc.ListDistributions(ctx, distributionActor(), 6)
	require.NoError(t, err)
	require.Len(t, resp.Items, 2)
	assert.Equal(t, 12, resp.Items[0].ID)
	assert.Equal(t, "published", resp.Items[0].CustomerTemplateStatus)
	assert.Equal(t, "1.2.0", resp.Items[0].LatestProviderVersion)
	assert.True(t, resp.Items[0].UpgradeAvailable)
	assert.Equal(t, "archived", resp.Items[1].CustomerTemplateStatus)
	assert.False(t, resp.Items[1].UpgradeAvailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListDistributions_AccessDenied(t *testing.T) {
	svc, _ := newDistributionServiceForTest(t, stubCustomerAccessChecker{err: NewCustomerAccessError(CodeMSPAllocationRequired, "x")})
	_, err := svc.ListDistributions(context.Background(), distributionActor(), 6)
	ae, ok := AsCustomerAccessError(err)
	require.True(t, ok)
	assert.Equal(t, CodeMSPAllocationRequired, ae.Code)
}
