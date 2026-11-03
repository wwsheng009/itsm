package middleware

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newIdempotencyTestRouter(db *sql.DB, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", 1)
		c.Set("user_id", 2)
		c.Next()
	})
	router.Use(IdempotencyMiddleware(db, zap.NewNop().Sugar()))
	router.POST("/items", handler)
	return router
}

func TestIdempotencyMiddlewareReplaysCompletedResponse(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	calls := 0
	router := newIdempotencyTestRouter(db, func(c *gin.Context) {
		calls++
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"id": 1}})
	})

	body := []byte(`{"title":"hello"}`)
	requestURL := "/items"
	hash := idempotencyRequestHash(httptest.NewRequest(http.MethodPost, requestURL, bytes.NewReader(body)), body)

	mock.ExpectQuery("INSERT INTO http_idempotency_records").
		WithArgs(1, 2, http.MethodPost, requestURL, "k1", hash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec("UPDATE http_idempotency_records").
		WithArgs(http.StatusOK, sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, requestURL, bytes.NewReader(body))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(first, req)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Equal(t, 1, calls)
	firstBody := append([]byte(nil), first.Body.Bytes()...)

	mock.ExpectQuery("INSERT INTO http_idempotency_records").
		WithArgs(1, 2, http.MethodPost, requestURL, "k1", hash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id, request_hash, state, status_code").
		WithArgs(1, 2, http.MethodPost, requestURL, "k1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "request_hash", "state", "status_code", "response_body", "response_content_type", "created_at", "expires_at",
		}).AddRow(1, hash, "completed", http.StatusOK, string(firstBody), "application/json; charset=utf-8",
			time.Now(), time.Now().Add(time.Hour)))

	second := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, requestURL, bytes.NewReader(body))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(second, req)

	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Equal(t, 1, calls, "重放不得再次执行业务 handler")
	require.Equal(t, "true", second.Header().Get("Idempotency-Replayed"))
	require.JSONEq(t, string(firstBody), second.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIdempotencyMiddlewareRejectsDifferentPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	calls := 0
	router := newIdempotencyTestRouter(db, func(c *gin.Context) {
		calls++
		c.Status(http.StatusOK)
	})

	body := []byte(`{"title":"new"}`)
	mock.ExpectQuery("INSERT INTO http_idempotency_records").
		WithArgs(1, 2, http.MethodPost, "/items", "k1", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id, request_hash, state, status_code").
		WithArgs(1, 2, http.MethodPost, "/items", "k1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "request_hash", "state", "status_code", "response_body", "response_content_type", "created_at", "expires_at",
		}).AddRow(1, "different-hash", "completed", http.StatusOK, `{}`, "application/json",
			time.Now(), time.Now().Add(time.Hour)))

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/items", bytes.NewReader(body))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Zero(t, calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIdempotencyMiddlewareSkipsMultipart(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	calls := 0
	router := newIdempotencyTestRouter(db, func(c *gin.Context) {
		calls++
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/items", bytes.NewReader([]byte("file")))
	req.Header.Set(IdempotencyKeyHeader, "upload-1")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, calls)
	require.NoError(t, mock.ExpectationsWereMet(), "multipart 上传不应触发幂等 SQL")
}

func TestIdempotencyMiddlewareSkipsAuthRoutes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	calls := 0
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", 1)
		c.Set("user_id", 2)
		c.Next()
	})
	router.Use(IdempotencyMiddleware(db, zap.NewNop().Sugar()))
	router.POST("/api/v1/auth/switch-tenant", func(c *gin.Context) {
		calls++
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/switch-tenant", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, calls)
	require.NoError(t, mock.ExpectationsWereMet(), "认证/会话端点不得进入幂等 SQL")
}

func TestIdempotencyMiddlewareReclaimsExpiredRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	calls := 0
	router := newIdempotencyTestRouter(db, func(c *gin.Context) {
		calls++
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"id": 2}})
	})

	body := []byte(`{"title":"expired"}`)
	hash := idempotencyRequestHash(httptest.NewRequest(http.MethodPost, "/items", bytes.NewReader(body)), body)
	mock.ExpectQuery("INSERT INTO http_idempotency_records").
		WithArgs(1, 2, http.MethodPost, "/items", "k1", hash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id, request_hash, state, status_code").
		WithArgs(1, 2, http.MethodPost, "/items", "k1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "request_hash", "state", "status_code", "response_body", "response_content_type", "created_at", "expires_at",
		}).AddRow(1, hash, "completed", http.StatusOK, `{}`, "application/json",
			time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)))
	mock.ExpectExec("DELETE FROM http_idempotency_records").
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO http_idempotency_records").
		WithArgs(1, 2, http.MethodPost, "/items", "k1", hash, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
	mock.ExpectExec("UPDATE http_idempotency_records").
		WithArgs(http.StatusOK, sqlmock.AnyArg(), sqlmock.AnyArg(), 2).
		WillReturnResult(sqlmock.NewResult(0, 1))

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/items", bytes.NewReader(body))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, calls, "过期记录应释放后重新执行，而不是无限重放")
	require.NoError(t, mock.ExpectationsWereMet())
}
