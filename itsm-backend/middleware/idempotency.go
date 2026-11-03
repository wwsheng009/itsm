package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"itsm-backend/common"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	// IdempotencyKeyHeader 是 HTTP 幂等键头名（RFC 草案与主流 API 约定）。
	IdempotencyKeyHeader = "Idempotency-Key"

	idempotencyTTL               = 24 * time.Hour
	idempotencyProcessingTimeout = 2 * time.Minute
	idempotencyMaxKeyLength      = 128
	idempotencyMaxResponseBytes  = 2 * 1024 * 1024
)

// idempotencyScope 是幂等记录的唯一作用域。
type idempotencyScope struct {
	tenantID int
	userID   int
	method   string
	path     string
	key      string
}

// idempotencyRecord 是已存在的幂等记录。
type idempotencyRecord struct {
	id           int64
	requestHash  string
	state        string
	statusCode   int
	responseBody []byte
	contentType  string
	createdAt    time.Time
	expiresAt    time.Time
}

// IdempotencyStore 基于 PostgreSQL 的 HTTP 幂等记录存储。
type IdempotencyStore struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time
}

// NewIdempotencyStore 创建幂等记录存储（db 为 nil 时由中间件降级为透传）。
func NewIdempotencyStore(db *sql.DB) *IdempotencyStore {
	return &IdempotencyStore{
		db:  db,
		ttl: idempotencyTTL,
		now: time.Now,
	}
}

// IdempotencyMiddleware 对携带 Idempotency-Key 的非安全请求做结果重放。
//
// 语义：
//   - 仅在方法为 POST/PUT/PATCH/DELETE 且 Content-Type 非 multipart 时生效；
//   - 第一次请求以唯一索引占位，成功后缓存 2xx 响应（24h）；
//   - 同 key + 同请求体重放原响应；同 key + 不同请求体返回 409；
//   - 并发同 key 返回 409 + Retry-After: 1；失败响应不缓存，允许修正后重试；
//   - multipart 上传走各宿主自身的 clientToken 幂等，不在此处缓存文件流。
func IdempotencyMiddleware(db *sql.DB, logger *zap.SugaredLogger) gin.HandlerFunc {
	if db == nil {
		return func(c *gin.Context) { c.Next() }
	}
	store := NewIdempotencyStore(db)
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	return func(c *gin.Context) {
		if !idempotencyEligible(c.Request) {
			c.Next()
			return
		}
		key := strings.TrimSpace(c.GetHeader(IdempotencyKeyHeader))
		if key == "" {
			c.Next()
			return
		}
		if len(key) > idempotencyMaxKeyLength {
			common.Fail(c, common.ParamErrorCode, "Idempotency-Key 长度不能超过 128")
			return
		}
		tenantID, tenantOK := idempotencyIntContext(c, "tenant_id")
		userID, userOK := idempotencyIntContext(c, "user_id")
		if !tenantOK || !userOK {
			// 认证路由应已注入；缺失时不阻断业务，交由后续鉴权/处理器给出准确响应。
			c.Next()
			return
		}

		body, err := idempotencyReadBody(c.Request)
		if err != nil {
			common.Fail(c, common.ParamErrorCode, "请求体读取失败")
			return
		}
		scope := idempotencyScope{
			tenantID: tenantID,
			userID:   userID,
			method:   c.Request.Method,
			path:     c.Request.URL.Path,
			key:      key,
		}
		requestHash := idempotencyRequestHash(c.Request, body)

		for attempt := 0; attempt < 2; attempt++ {
			recordID, acquired, acquireErr := store.acquire(c.Request.Context(), scope, requestHash)
			if acquireErr != nil {
				logger.Errorw("创建幂等记录失败，拒绝写请求以避免重复",
					"method", scope.method, "path", scope.path, "tenant_id", scope.tenantID, "error", acquireErr)
				common.Fail(c, common.ServiceUnavailableCode, "幂等保护暂不可用，请稍后重试")
				return
			}
			if acquired {
				c.Header(IdempotencyKeyHeader, key)
				store.executeAndPersist(c, logger, recordID)
				return
			}

			record, getErr := store.get(c.Request.Context(), scope)
			if errors.Is(getErr, sql.ErrNoRows) {
				continue
			}
			if getErr != nil {
				logger.Errorw("读取幂等记录失败",
					"method", scope.method, "path", scope.path, "tenant_id", scope.tenantID, "error", getErr)
				common.Fail(c, common.ServiceUnavailableCode, "幂等保护暂不可用，请稍后重试")
				return
			}
			if store.now().After(record.expiresAt) {
				if deleteErr := store.deleteByID(c.Request.Context(), record.id); deleteErr != nil {
					logger.Warnw("清理过期幂等记录失败", "record_id", record.id, "error", deleteErr)
					common.Fail(c, common.ServiceUnavailableCode, "幂等保护暂不可用，请稍后重试")
					return
				}
				continue
			}
			if record.requestHash != requestHash {
				c.Header(IdempotencyKeyHeader, key)
				common.Fail(c, common.ConflictCode, "相同 Idempotency-Key 已用于不同的请求内容")
				return
			}
			if record.state == "completed" {
				c.Header(IdempotencyKeyHeader, key)
				c.Header("Idempotency-Replayed", "true")
				if record.contentType != "" {
					c.Header("Content-Type", record.contentType)
				}
				c.Status(record.statusCode)
				_, _ = c.Writer.Write(record.responseBody)
				c.Abort()
				return
			}
			if record.state == "processing" {
				if store.now().Sub(record.createdAt) > idempotencyProcessingTimeout {
					if deleteErr := store.deleteByID(c.Request.Context(), record.id); deleteErr != nil {
						logger.Warnw("清理超时幂等占位失败", "record_id", record.id, "error", deleteErr)
					}
					continue
				}
				c.Header(IdempotencyKeyHeader, key)
				c.Header("Retry-After", "1")
				common.Fail(c, common.ConflictCode, "相同 Idempotency-Key 的请求正在处理中，请稍后重试")
				return
			}
			// 未知状态（历史脏数据）：删除后重试占位。
			if deleteErr := store.deleteByID(c.Request.Context(), record.id); deleteErr != nil {
				logger.Warnw("清理未知状态幂等记录失败", "record_id", record.id, "error", deleteErr)
				common.Fail(c, common.ServiceUnavailableCode, "幂等保护暂不可用，请稍后重试")
				return
			}
		}

		common.Fail(c, common.ServiceUnavailableCode, "幂等保护暂不可用，请稍后重试")
	}
}

// executeAndPersist 执行后续 handler，并把 2xx 响应写入幂等记录。
func (s *IdempotencyStore) executeAndPersist(c *gin.Context, logger *zap.SugaredLogger, recordID int64) {
	writer := &idempotencyResponseWriter{ResponseWriter: c.Writer}
	original := c.Writer
	c.Writer = writer
	defer func() { c.Writer = original }()

	c.Next()

	status := writer.Status()
	if status < 100 {
		status = http.StatusOK
	}
	// 认证/会话类响应可能携带 Set-Cookie；重放 body 但不重放 Cookie 会造成
	// 「客户端以为已切换租户/已刷新会话」的假成功，因此不缓存此类响应。
	if len(writer.Header().Values("Set-Cookie")) > 0 {
		if err := s.deleteByID(c.Request.Context(), recordID); err != nil {
			logger.Warnw("释放带 Set-Cookie 的幂等占位失败", "record_id", recordID, "error", err)
		}
		return
	}
	if status < 200 || status >= 300 || writer.tooLarge {
		if err := s.deleteByID(c.Request.Context(), recordID); err != nil {
			logger.Warnw("释放未缓存的幂等占位失败", "record_id", recordID, "error", err)
		}
		return
	}
	if err := s.complete(c.Request.Context(), recordID, status, writer.Header().Get("Content-Type"), writer.body.Bytes()); err != nil {
		// 响应已发出，无法回滚；记录保持 processing，超时后下一次重试会重新占位。
		logger.Errorw("保存幂等响应失败", "record_id", recordID, "error", err)
	}
}

// acquire 尝试插入 processing 占位；返回 (recordID, true, nil) 表示本次请求获胜。
func (s *IdempotencyStore) acquire(ctx context.Context, scope idempotencyScope, requestHash string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO http_idempotency_records
			(tenant_id, user_id, method, path, idempotency_key, request_hash, state, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'processing', $7)
		ON CONFLICT (tenant_id, user_id, method, path, idempotency_key) DO NOTHING
		RETURNING id`,
		scope.tenantID, scope.userID, scope.method, scope.path, scope.key, requestHash, s.now().Add(s.ttl),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (s *IdempotencyStore) get(ctx context.Context, scope idempotencyScope) (*idempotencyRecord, error) {
	var record idempotencyRecord
	var body string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, request_hash, state, status_code,
		       COALESCE(response_body, ''), COALESCE(response_content_type, ''), created_at, expires_at
		FROM http_idempotency_records
		WHERE tenant_id = $1 AND user_id = $2 AND method = $3 AND path = $4 AND idempotency_key = $5`,
		scope.tenantID, scope.userID, scope.method, scope.path, scope.key,
	).Scan(&record.id, &record.requestHash, &record.state, &record.statusCode,
		&body, &record.contentType, &record.createdAt, &record.expiresAt)
	if err != nil {
		return nil, err
	}
	record.responseBody = []byte(body)
	return &record, nil
}

func (s *IdempotencyStore) complete(ctx context.Context, id int64, statusCode int, contentType string, body []byte) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE http_idempotency_records
		SET state = 'completed', status_code = $1, response_body = $2,
		    response_content_type = $3, completed_at = NOW()
		WHERE id = $4`,
		statusCode, string(body), contentType, id)
	return err
}

func (s *IdempotencyStore) deleteByID(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM http_idempotency_records WHERE id = $1`, id)
	return err
}

// idempotencyEligible 判断请求是否需要幂等保护。
func idempotencyEligible(r *http.Request) bool {
	if r == nil {
		return false
	}
	// 会话/认证端点不做结果重放：它们依赖 Cookie/CSRF 等响应副作用。
	if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	if strings.TrimSpace(r.Header.Get("Upgrade")) != "" {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	return contentType != "multipart/form-data"
}

func idempotencyReadBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, common.DefaultRequestMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > common.DefaultRequestMaxBytes {
		return nil, errors.New("request body too large")
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func idempotencyRequestHash(r *http.Request, body []byte) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(r.Method))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(r.URL.RequestURI()))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write(body)
	return hex.EncodeToString(hasher.Sum(nil))
}

func idempotencyIntContext(c *gin.Context, key string) (int, bool) {
	value, ok := c.Get(key)
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, typed > 0
	case int64:
		return int(typed), typed > 0
	default:
		return 0, false
	}
}

// idempotencyResponseWriter 在写回客户端的同时缓存响应体（有上限）。
type idempotencyResponseWriter struct {
	gin.ResponseWriter
	body     bytes.Buffer
	tooLarge bool
}

func (w *idempotencyResponseWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *idempotencyResponseWriter) WriteString(value string) (int, error) {
	w.capture([]byte(value))
	return w.ResponseWriter.WriteString(value)
}

func (w *idempotencyResponseWriter) capture(data []byte) {
	if w.tooLarge {
		return
	}
	if w.body.Len()+len(data) > idempotencyMaxResponseBytes {
		w.tooLarge = true
		w.body.Reset()
		return
	}
	_, _ = w.body.Write(data)
}
