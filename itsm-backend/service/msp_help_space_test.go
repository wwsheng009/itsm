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

	"itsm-backend/dto"
)

// P2 MSP 帮助空间与分发授权：服务层契约（sqlmock，不依赖真实数据库）。
//
// 覆盖：authoring 生命周期（draft→publish→编辑回 draft）/ 分发 copy-in / 升级提示与显式升级 /
// 解绑转原生 / 客户侧消费（页面 + 检索片段）/ 授权与参数 fail-closed。

type stubCustomerAccess struct {
	err            error
	calls          int
	lastUserID     int
	lastCustomerID int
}

func (s *stubCustomerAccess) CanAccessCustomer(_ context.Context, userID, customerTenantID int) error {
	s.calls++
	s.lastUserID = userID
	s.lastCustomerID = customerTenantID
	return s.err
}

func newMSPHelpService(t *testing.T) (*MSPHelpSpaceService, sqlmock.Sqlmock, *stubCustomerAccess) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	guard := &stubCustomerAccess{}
	return NewMSPHelpSpaceService(db, guard, nil, nil), mock, guard
}

var helpDocRowCols = []string{
	"id", "tenant_id", "slug", "title", "page_key", "body_md", "status", "version",
	"created_by", "updated_by", "created_at", "updated_at", "published_at",
}

var helpDistRowCols = []string{
	"id", "tenant_id", "provider_tenant_id", "source_doc_id", "source_slug", "source_version",
	"title", "page_key", "status", "distributed_by", "distributed_at", "unbound_by", "unbound_at",
}

func docRows() *sqlmock.Rows {
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	return sqlmock.NewRows(helpDocRowCols).
		AddRow(11, 7, "sla-guide", "SLA 指南", "sla.monitor", "# SLA\n\n临近违约处理", "draft", 1, 9, 9, now, now, nil)
}

func TestMSPHelpSpace_AuthoringLifecycle(t *testing.T) {
	svc, mock, _ := newMSPHelpService(t)
	actor := MSPWorkbenchActor{UserID: 9, HomeTenantID: 7, MSPRole: "provider_admin"}
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	// 创建：slug 不存在 → 插入草稿。
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM msp_help_docs")).
		WithArgs(int64(7), "sla-guide").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO msp_help_docs")).
		WithArgs(int64(7), "sla-guide", "SLA 指南", "sla.monitor", "# SLA\n\n临近违约处理", int64(9)).
		WillReturnRows(docRows())

	doc, err := svc.CreateDoc(context.Background(), actor, dto.MSPHelpDocRequest{
		Slug: "sla-guide", Title: "SLA 指南", PageKey: "sla.monitor", BodyMD: "# SLA\n\n临近违约处理",
	})
	require.NoError(t, err)
	assert.Equal(t, 11, doc.ID)
	assert.Equal(t, "draft", doc.Status)
	assert.Equal(t, 1, doc.Version)

	// slug 冲突：存在 → ErrMSPHelpDocConflict（不触库写入）。
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM msp_help_docs")).
		WithArgs(int64(7), "sla-guide").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	_, err = svc.CreateDoc(context.Background(), actor, dto.MSPHelpDocRequest{Slug: "sla-guide", Title: "x"})
	assert.ErrorIs(t, err, ErrMSPHelpDocConflict)

	// 参数 fail-closed：大写 slug 直接拒绝（不触库）。
	_, err = svc.CreateDoc(context.Background(), actor, dto.MSPHelpDocRequest{Slug: "SLA-Guide", Title: "x"})
	assert.ErrorIs(t, err, ErrMSPHelpDocInvalid)

	// 发布：draft → published。
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE msp_help_docs SET")).
		WithArgs(int64(7), int64(11), int64(9)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(11, 7, "sla-guide", "SLA 指南", "sla.monitor", "# SLA", "published", 1, 9, 9, now, now, now))
	published, err := svc.PublishDoc(context.Background(), actor, 11)
	require.NoError(t, err)
	assert.Equal(t, "published", published.Status)
	require.NotNil(t, published.PublishedAt)

	// 已发布文档编辑 → 回 draft 且版本推进（v2）。
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE msp_help_docs SET")).
		WithArgs(int64(7), int64(11), "SLA 指南 v2", "sla.monitor", "# SLA v2", int64(9)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(11, 7, "sla-guide", "SLA 指南 v2", "sla.monitor", "# SLA v2", "draft", 2, 9, 9, now, now, now))
	updated, err := svc.UpdateDoc(context.Background(), actor, 11, dto.MSPHelpDocRequest{Title: "SLA 指南 v2", PageKey: "sla.monitor", BodyMD: "# SLA v2"})
	require.NoError(t, err)
	assert.Equal(t, "draft", updated.Status)
	assert.Equal(t, 2, updated.Version)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMSPHelpSpace_Distribute_CopyInAndAuthorization(t *testing.T) {
	svc, mock, guard := newMSPHelpService(t)
	actor := MSPWorkbenchActor{UserID: 9, HomeTenantID: 7, Username: "admin", MSPRole: "provider_admin"}
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	distributedAt := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	// 授权通过 → 读取 provider 已发布文档 → active 不存在 → 落 copy-in 快照。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_docs WHERE tenant_id=$1 AND id=$2")).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(11, 7, "sla-guide", "SLA 指南", "sla.monitor", "# SLA", "published", 3, 9, 9, now, now, now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM msp_help_distributions")).
		WithArgs(int64(6), "sla-guide").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO msp_help_distributions")).
		WithArgs(int64(6), int64(7), int64(11), "sla-guide", int64(3), now, "SLA 指南", "sla.monitor", "# SLA", int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "distributed_at"}).AddRow(21, distributedAt))

	dist, err := svc.Distribute(context.Background(), actor, 6, 11)
	require.NoError(t, err)
	assert.Equal(t, 21, dist.ID)
	assert.Equal(t, 6, dist.CustomerTenantID)
	assert.Equal(t, "active", dist.Status)
	assert.Equal(t, 3, dist.SourceVersion)
	assert.False(t, dist.UpgradeAvailable)
	assert.Equal(t, 1, guard.calls)
	assert.Equal(t, 9, guard.lastUserID)
	assert.Equal(t, 6, guard.lastCustomerID)

	// 已有 active 分发 → 冲突（不覆盖未决副本）。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_docs WHERE tenant_id=$1 AND id=$2")).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(11, 7, "sla-guide", "SLA 指南", "sla.monitor", "# SLA", "published", 3, 9, 9, now, now, now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM msp_help_distributions")).
		WithArgs(int64(6), "sla-guide").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	_, err = svc.Distribute(context.Background(), actor, 6, 11)
	assert.ErrorIs(t, err, ErrMSPHelpDocConflict)

	// 未发布文档不可分发。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_docs WHERE tenant_id=$1 AND id=$2")).
		WithArgs(int64(7), int64(12)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(12, 7, "draft-doc", "草稿", "", "x", "draft", 1, 9, 9, now, now, nil))
	_, err = svc.Distribute(context.Background(), actor, 6, 12)
	assert.ErrorIs(t, err, ErrMSPHelpDocNotPublished)

	// 授权失败：原样传播（mspguard 错误），不触库。
	guard.err = errors.New("access denied: no active allocation")
	_, err = svc.Distribute(context.Background(), actor, 6, 11)
	assert.ErrorContains(t, err, "access denied")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMSPHelpSpace_ListDistributions_UpgradeHint(t *testing.T) {
	svc, mock, _ := newMSPHelpService(t)
	actor := MSPWorkbenchActor{UserID: 9, HomeTenantID: 7}
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND provider_tenant_id=$2")).
		WithArgs(int64(6), int64(7)).
		WillReturnRows(sqlmock.NewRows(helpDistRowCols).
			AddRow(21, 6, 7, 11, "sla-guide", 2, "SLA 指南", "sla.monitor", "active", 9, now, 0, nil).
			AddRow(20, 6, 7, 12, "old-doc", 1, "旧文档", "", "unbound", 9, now, 9, now))
	// provider 最新已发布版本：sla-guide=3（可升级）；old-doc 已下线=0（无提示）。
	mock.ExpectQuery(regexp.QuoteMeta("SELECT version FROM msp_help_docs")).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(3))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT version FROM msp_help_docs")).
		WithArgs(int64(7), int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"version"}))

	resp, err := svc.ListDistributions(context.Background(), actor, 6)
	require.NoError(t, err)
	require.Len(t, resp.Items, 2)
	assert.Equal(t, 3, resp.Items[0].LatestProviderVersion)
	assert.True(t, resp.Items[0].UpgradeAvailable)
	assert.False(t, resp.Items[1].UpgradeAvailable)
	assert.Equal(t, "unbound", resp.Items[1].Status)
	require.NotNil(t, resp.Items[1].UnboundAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMSPHelpSpace_UpgradeAndUnbind(t *testing.T) {
	svc, mock, _ := newMSPHelpService(t)
	actor := MSPWorkbenchActor{UserID: 9, HomeTenantID: 7}
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	// 升级：active 分发（v2）→ provider 最新已发布 v3 → 原位刷新快照。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND id=$2 AND provider_tenant_id=$3")).
		WithArgs(int64(6), int64(21), int64(7)).
		WillReturnRows(sqlmock.NewRows(helpDistRowCols).
			AddRow(21, 6, 7, 11, "sla-guide", 2, "SLA 指南 v2", "sla.monitor", "active", 9, now, 0, nil))
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_docs WHERE tenant_id=$1 AND id=$2")).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(11, 7, "sla-guide", "SLA 指南 v3", "sla.monitor", "# v3", "published", 3, 9, 9, now, now, now))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE msp_help_distributions SET")).
		WithArgs(int64(6), int64(21), int64(11), int64(3), now, "SLA 指南 v3", "sla.monitor", "# v3").
		WillReturnResult(sqlmock.NewResult(0, 1))

	dist, err := svc.Upgrade(context.Background(), actor, 6, 21)
	require.NoError(t, err)
	assert.Equal(t, 3, dist.SourceVersion)
	assert.Equal(t, "SLA 指南 v3", dist.Title)

	// 无可用升级（provider 仍是 v2）→ 冲突语义。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND id=$2 AND provider_tenant_id=$3")).
		WithArgs(int64(6), int64(21), int64(7)).
		WillReturnRows(sqlmock.NewRows(helpDistRowCols).
			AddRow(21, 6, 7, 11, "sla-guide", 2, "SLA 指南 v2", "sla.monitor", "active", 9, now, 0, nil))
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_docs WHERE tenant_id=$1 AND id=$2")).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(11, 7, "sla-guide", "SLA 指南 v2", "sla.monitor", "# v2", "published", 2, 9, 9, now, now, now))
	_, err = svc.Upgrade(context.Background(), actor, 6, 21)
	assert.ErrorIs(t, err, ErrMSPHelpDocConflict)

	// 解绑：active → unbound（保留快照，转客户原生）。
	unboundAt := time.Date(2026, 10, 18, 11, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND id=$2 AND provider_tenant_id=$3")).
		WithArgs(int64(6), int64(21), int64(7)).
		WillReturnRows(sqlmock.NewRows(helpDistRowCols).
			AddRow(21, 6, 7, 11, "sla-guide", 2, "SLA 指南 v2", "sla.monitor", "active", 9, now, 0, nil))
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_docs WHERE tenant_id=$1 AND id=$2")).
		WithArgs(int64(7), int64(11)).
		WillReturnRows(sqlmock.NewRows(helpDocRowCols).
			AddRow(11, 7, "sla-guide", "SLA 指南 v2", "sla.monitor", "# v2", "published", 2, 9, 9, now, now, now))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE msp_help_distributions SET")).
		WithArgs(int64(6), int64(21), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"unbound_at"}).AddRow(unboundAt))

	unbound, err := svc.Unbind(context.Background(), actor, 6, 21)
	require.NoError(t, err)
	assert.Equal(t, "unbound", unbound.Status)
	require.NotNil(t, unbound.UnboundAt)
	assert.Equal(t, unboundAt, *unbound.UnboundAt)

	// 已解绑的分发不可再升级/解绑（状态冲突）。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND id=$2 AND provider_tenant_id=$3")).
		WithArgs(int64(6), int64(21), int64(7)).
		WillReturnRows(sqlmock.NewRows(helpDistRowCols).
			AddRow(21, 6, 7, 11, "sla-guide", 2, "SLA 指南 v2", "sla.monitor", "unbound", 9, now, 9, unboundAt))
	_, err = svc.Upgrade(context.Background(), actor, 6, 21)
	assert.ErrorIs(t, err, ErrMSPHelpDocConflict)

	// 不存在的分发行：同租户口径 404 语义。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND id=$2 AND provider_tenant_id=$3")).
		WithArgs(int64(6), int64(99), int64(7)).
		WillReturnRows(sqlmock.NewRows(helpDistRowCols))
	_, err = svc.Unbind(context.Background(), actor, 6, 99)
	assert.ErrorIs(t, err, ErrMSPHelpDocNotFound)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMSPHelpSpace_CustomerConsumption(t *testing.T) {
	svc, mock, _ := newMSPHelpService(t)

	// 页面帮助：active + unbound 都可见（unbound=客户原生副本），按 pageKey 过滤。
	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND status IN")).
		WithArgs(int64(6), "ticket.list").
		WillReturnRows(sqlmock.NewRows([]string{"title", "page_key", "body_md", "source_slug", "source_version", "provider_tenant_id", "status"}).
			AddRow("服务商入库指引", "ticket.list", "# 入库指引", "intake-guide", 2, 7, "active").
			AddRow("历史副本", "ticket.list", "# 历史", "legacy-guide", 1, 7, "unbound"))

	docs, err := svc.CustomerHelpDocs(context.Background(), 6, "ticket.list")
	require.NoError(t, err)
	require.Len(t, docs, 2)
	assert.Equal(t, "active", docs[0].Status)
	assert.Equal(t, "unbound", docs[1].Status)
	assert.Equal(t, 7, docs[0].ProviderTenantID)

	// 检索：ILIKE 子串 + 片段。
	mock.ExpectQuery(regexp.QuoteMeta("title ILIKE $2")).
		WithArgs(int64(6), "%入库%", int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"title", "page_key", "body_md", "source_slug", "source_version", "provider_tenant_id", "status"}).
			AddRow("服务商入库指引", "ticket.list", "# 入库指引\n\n先登记资产再入库", "intake-guide", 2, 7, "active"))

	results, err := svc.SearchCustomerHelpDocs(context.Background(), 6, "入库", "", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Snippet, "入库")

	assert.NoError(t, mock.ExpectationsWereMet())

	// 未装配/空检索：零库访问、空结果（帮助视图降级）。
	empty := NewMSPHelpSpaceService(nil, nil, nil, nil)
	docs, err = empty.CustomerHelpDocs(context.Background(), 6, "x")
	require.NoError(t, err)
	assert.Empty(t, docs)
	results, err = empty.SearchCustomerHelpDocs(context.Background(), 6, "", "", 5)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestHelpMatchSnippet(t *testing.T) {
	body := "第一段\n\n先登记资产再入库，随后核对数量。\n\n第三段"
	snippet := helpMatchSnippet(body, "入库")
	assert.Contains(t, snippet, "入库")

	longBody := ""
	for i := 0; i < 300; i++ {
		longBody += "字"
	}
	longSnippet := helpMatchSnippet(longBody, "不存在")
	assert.LessOrEqual(t, len([]rune(longSnippet)), mspHelpSnippetRunes+1) // +1 为省略号
}
