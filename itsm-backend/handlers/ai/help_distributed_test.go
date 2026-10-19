package ai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/service"
)

// P2 客户侧帮助合并：服务商分发文档进入 /ai/help/page 与 /ai/help/search
//（未装配/查询失败 → 空数组，不阻塞内嵌帮助视图）。

const helpDistReadCols = "title,page_key,body_md,source_slug,source_version,provider_tenant_id,status"

func newDistributedHelpRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	helpSvc := service.NewMSPHelpSpaceService(db, nil, nil, zap.NewNop().Sugar())

	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.SetMSPHelpSpace(helpSvc)
	h := NewHandler(svc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/ai/help/page", func(c *gin.Context) {
		c.Set("tenant_id", 6)
		h.GetPageHelp(c)
	})
	r.GET("/api/v1/ai/help/search", func(c *gin.Context) {
		c.Set("tenant_id", 6)
		h.SearchHelp(c)
	})
	return r, mock, func() { _ = db.Close() }
}

type distributedHelpPayload struct {
	Code int `json:"code"`
	Data struct {
		Docs []struct {
			Path string `json:"path"`
			Text string `json:"text"`
		} `json:"docs"`
		DistributedDocs []struct {
			Title            string `json:"title"`
			PageKey          string `json:"pageKey"`
			Text             string `json:"text"`
			Source           string `json:"source"`
			ProviderTenantID int    `json:"providerTenantId"`
			SourceSlug       string `json:"sourceSlug"`
			SourceVersion    int    `json:"sourceVersion"`
			Status           string `json:"status"`
		} `json:"distributedDocs"`
	} `json:"data"`
}

type distributedSearchPayload struct {
	Code int `json:"code"`
	Data struct {
		Results            []map[string]interface{} `json:"results"`
		DistributedResults []struct {
			Title            string `json:"title"`
			PageKey          string `json:"pageKey"`
			Snippet          string `json:"snippet"`
			Source           string `json:"source"`
			ProviderTenantID int    `json:"providerTenantId"`
			Status           string `json:"status"`
		} `json:"distributedResults"`
	} `json:"data"`
}

func TestGetPageHelp_MergesDistributedDocs(t *testing.T) {
	r, mock, closeDB := newDistributedHelpRouter(t)
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND status IN")).
		WithArgs(int64(6), "ticket.list").
		WillReturnRows(sqlmock.NewRows([]string{
			"title", "page_key", "body_md", "source_slug", "source_version", "provider_tenant_id", "status",
		}).
			AddRow("服务商入库指引", "ticket.list", "# 入库指引\n\n先登记资产再入库", "intake-guide", 2, 7, "active").
			AddRow("历史副本", "ticket.list", "# 历史", "legacy-guide", 1, 7, "unbound"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/help/page?pageKey=ticket.list", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp distributedHelpPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	require.Len(t, resp.Data.DistributedDocs, 2)
	assert.Equal(t, "服务商入库指引", resp.Data.DistributedDocs[0].Title)
	assert.Equal(t, "msp_provider", resp.Data.DistributedDocs[0].Source)
	assert.Equal(t, 7, resp.Data.DistributedDocs[0].ProviderTenantID)
	assert.Equal(t, 2, resp.Data.DistributedDocs[0].SourceVersion)
	assert.Equal(t, "active", resp.Data.DistributedDocs[0].Status)
	assert.Equal(t, "unbound", resp.Data.DistributedDocs[1].Status)
	assert.Contains(t, resp.Data.DistributedDocs[0].Text, "入库指引")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchHelp_MergesDistributedResults(t *testing.T) {
	r, mock, closeDB := newDistributedHelpRouter(t)
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta("title ILIKE $2")).
		WithArgs(int64(6), "%入库%", int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{
			"title", "page_key", "body_md", "source_slug", "source_version", "provider_tenant_id", "status",
		}).AddRow("服务商入库指引", "ticket.list", "# 入库指引\n\n先登记资产再入库", "intake-guide", 2, 7, "active"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/help/search?q=%E5%85%A5%E5%BA%93", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp distributedSearchPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	require.Len(t, resp.Data.DistributedResults, 1)
	assert.Equal(t, "服务商入库指引", resp.Data.DistributedResults[0].Title)
	assert.Contains(t, resp.Data.DistributedResults[0].Snippet, "入库")
	assert.Equal(t, "msp_provider", resp.Data.DistributedResults[0].Source)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDistributedHelp_DegradesWithoutService(t *testing.T) {
	// 未注入帮助空间服务：distributedDocs 为空数组，内嵌帮助照常（零库访问）。
	svc := NewService(nil, zap.NewNop().Sugar(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewHandler(svc)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/ai/help/page", func(c *gin.Context) {
		c.Set("tenant_id", 6)
		h.GetPageHelp(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/help/page?pageKey=ticket.list", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp distributedHelpPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	assert.Empty(t, resp.Data.DistributedDocs)
}

func TestGetPageHelp_DistributedQueryFailureDegrades(t *testing.T) {
	// 查询失败：200 + 空数组（帮助视图不被观测面拖垮）。
	r, mock, closeDB := newDistributedHelpRouter(t)
	defer closeDB()

	mock.ExpectQuery(regexp.QuoteMeta("FROM msp_help_distributions WHERE tenant_id=$1 AND status IN")).
		WithArgs(int64(6), "ticket.list").
		WillReturnError(errors.New("db down"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/help/page?pageKey=ticket.list", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp distributedHelpPayload
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	assert.Empty(t, resp.Data.DistributedDocs)
	assert.NoError(t, mock.ExpectationsWereMet())
}
