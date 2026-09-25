package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// 本文件覆盖主计划《多 LLM Provider 支持与可切换方案》v1.6 BE-4 §3.4 的管理 API 契约：
// 校验分支（422 协议/变体/adapter_options/endpoint/name/displayName）、冲突与默认约束
// （409 name 冲突 / 默认实例禁删禁禁用）、跨租户不可见（404）、掩码与密钥卫生
// （响应无明文/无密文、审计与错误文本不含明文）、个人默认解析链（user→tenant→static）、
// 连通性测试（key 缺失 422 / 桩服务 ok / 桩服务错误脱敏）与 import-static 幂等。
//
// 路由级 system:write 门禁的注册正确性由 router 包守卫测试覆盖
// （route_permission_guard_test.go 扫描到分组级中间件即通过）。

var llmAdminTestDBCounter int64

// llmAdminTestDSN 每个用例独立的内存库，避免连接池残留导致跨用例串数据。
func llmAdminTestDSN() string {
	return fmt.Sprintf("file:ai_admin_test_%d?mode=memory&cache=shared&_fk=1", atomic.AddInt64(&llmAdminTestDBCounter, 1))
}

// llmAdminTestCipher 可判定的窄接口替身：Encrypt 加前缀，Decrypt 校验前缀。
type llmAdminTestCipher struct{ prefix string }

func (c llmAdminTestCipher) Encrypt(plaintext string) (string, error) {
	return c.prefix + plaintext, nil
}

func (c llmAdminTestCipher) Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, c.prefix) || len(ciphertext) == len(c.prefix) {
		return "", errors.New("ciphertext malformed")
	}
	return strings.TrimPrefix(ciphertext, c.prefix), nil
}

type llmAdminTestInvalidator struct {
	mu      sync.Mutex
	tenants []int
}

func (i *llmAdminTestInvalidator) Invalidate(tenantID int) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.tenants = append(i.tenants, tenantID)
}

func (i *llmAdminTestInvalidator) calls() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.tenants)
}

type llmAdminTestAudit struct {
	mu    sync.Mutex
	kinds []string
	notes []string
}

func (a *llmAdminTestAudit) SaveFeedback(_ context.Context, _ int, _ int, _ string, kind, _ string, _ string, _ *int, _ bool, _ *int, notes *string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.kinds = append(a.kinds, kind)
	if notes != nil {
		a.notes = append(a.notes, *notes)
	}
	return nil
}

// actions 从审计 notes 的 JSON 中提取 action 序列（服务层记录格式固定）。
func (a *llmAdminTestAudit) actions(t *testing.T) []string {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.notes))
	for _, note := range a.notes {
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(note), &payload))
		action, _ := payload["action"].(string)
		out = append(out, action)
	}
	return out
}

func (a *llmAdminTestAudit) allNotes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.notes...)
}

type llmAdminTestEnv struct {
	svc    *ai.LLMProviderAdminService
	client *ent.Client
	cipher llmAdminTestCipher
	inv    *llmAdminTestInvalidator
	audit  *llmAdminTestAudit
}

const (
	llmAdminTenantID = 1
	llmAdminUserID   = 2
	llmAdminPlainKey = "sk-live-abcdefghijklmnop"
)

func newLLMAdminTestEnv(t *testing.T, opts ...func(*ai.LLMProviderAdminDeps)) *llmAdminTestEnv {
	t.Helper()
	client := enttest.Open(t, "sqlite3", llmAdminTestDSN())
	t.Cleanup(func() { _ = client.Close() })

	env := &llmAdminTestEnv{
		client: client,
		cipher: llmAdminTestCipher{prefix: "enc:"},
		inv:    &llmAdminTestInvalidator{},
		audit:  &llmAdminTestAudit{},
	}
	deps := ai.LLMProviderAdminDeps{
		Client:      client,
		Encrypter:   env.cipher,
		Invalidator: env.inv,
		Logger:      zap.NewNop().Sugar(),
		Audit:       env.audit,
	}
	for _, opt := range opts {
		opt(&deps)
	}
	env.svc = ai.NewLLMProviderAdminService(deps)
	return env
}

func llmAdminBoolPtr(v bool) *bool       { return &v }
func llmAdminStringPtr(v string) *string { return &v }

// validCreateRequest 返回一个可通过全部校验的创建请求（openai_chat_completions 默认变体）。
func validCreateRequest(name string) dto.LLMCreateProviderRequest {
	return dto.LLMCreateProviderRequest{
		Name:        name,
		DisplayName: "测试实例 " + name,
		Protocol:    service.LLMProtocolOpenAIChatCompletions,
		Model:       "gpt-4o-mini",
		APIKey:      llmAdminPlainKey,
	}
}

func mustCreateProvider(t *testing.T, env *llmAdminTestEnv, req dto.LLMCreateProviderRequest) dto.LLMProviderDTO {
	t.Helper()
	created, err := env.svc.CreateProvider(context.Background(), llmAdminTenantID, llmAdminUserID, req)
	require.NoError(t, err)
	return created
}

func requireLLMAdminError(t *testing.T, err error, status int, code string) {
	t.Helper()
	require.Error(t, err)
	var adminErr *ai.LLMAdminError
	require.True(t, errors.As(err, &adminErr), "err=%v", err)
	assert.Equal(t, status, adminErr.Status, "err=%v", err)
	assert.Equal(t, code, adminErr.Code, "err=%v", err)
}

// ---------- §3.4 校验分支：422 / 409 ----------

func TestLLMProviderAdminCreateValidationTable(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*dto.LLMCreateProviderRequest)
		wantStatus int
		wantCode   string
	}{
		{"name 非法", func(r *dto.LLMCreateProviderRequest) { r.Name = "Bad Name" }, 422, "AI_PROVIDER_NAME_INVALID"},
		{"name 超长", func(r *dto.LLMCreateProviderRequest) { r.Name = strings.Repeat("a", 65) }, 422, "AI_PROVIDER_NAME_INVALID"},
		{"displayName 超长", func(r *dto.LLMCreateProviderRequest) { r.DisplayName = strings.Repeat("x", 101) }, 422, "AI_PROVIDER_VALIDATION_ERROR"},
		{"google_gemini 变体非法（仅标准形态）", func(r *dto.LLMCreateProviderRequest) {
			r.Protocol = service.LLMProtocolGoogleGemini
			r.Variant = service.LLMVariantAzure
		}, 422, "AI_PROTOCOL_VARIANT_INVALID"},
		{"openai_responses 已实现且变体白名单只含标准项", func(r *dto.LLMCreateProviderRequest) {
			r.Protocol = service.LLMProtocolOpenAIResponses
			r.Variant = service.LLMVariantAzure
		}, 422, "AI_PROTOCOL_VARIANT_INVALID"},
		{"协议枚举非法", func(r *dto.LLMCreateProviderRequest) { r.Protocol = "chat_completions" }, 422, "AI_PROTOCOL_INVALID"},
		{"变体非法", func(r *dto.LLMCreateProviderRequest) { r.Variant = "vertex" }, 422, "AI_PROTOCOL_VARIANT_INVALID"},
		{"azure 缺 endpoint", func(r *dto.LLMCreateProviderRequest) { r.Variant = service.LLMVariantAzure }, 422, "AI_PROVIDER_ENDPOINT_REQUIRED"},
		{"azure 缺 deployment", func(r *dto.LLMCreateProviderRequest) {
			r.Variant = service.LLMVariantAzure
			r.Endpoint = "https://demo.openai.azure.com"
		}, 422, "AI_PROVIDER_VALIDATION_ERROR"},
		{"adapterOptions 敏感键", func(r *dto.LLMCreateProviderRequest) {
			r.AdapterOptions = json.RawMessage(`{"authorization":"Bearer x"}`)
		}, 422, "AI_ADAPTER_OPTIONS_INVALID"},
		{"adapterOptions 非对象", func(r *dto.LLMCreateProviderRequest) {
			r.AdapterOptions = json.RawMessage(`[1,2]`)
		}, 422, "AI_ADAPTER_OPTIONS_INVALID"},
		{"禁用实例不可设默认", func(r *dto.LLMCreateProviderRequest) {
			r.Enabled = llmAdminBoolPtr(false)
			r.IsDefault = true
		}, 422, "AI_PROVIDER_VALIDATION_ERROR"},
		{"协议大小写与空白归一后合法", func(r *dto.LLMCreateProviderRequest) { r.Protocol = " OpenAI_Chat_Completions " }, 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newLLMAdminTestEnv(t)
			req := validCreateRequest("main")
			tc.mutate(&req)

			created, err := env.svc.CreateProvider(context.Background(), llmAdminTenantID, llmAdminUserID, req)
			if tc.wantStatus == 0 {
				require.NoError(t, err)
				assert.Equal(t, service.LLMProtocolOpenAIChatCompletions, created.Protocol)
				return
			}
			requireLLMAdminError(t, err, tc.wantStatus, tc.wantCode)

			total, countErr := env.client.LLMProviderConfig.Query().Count(context.Background())
			require.NoError(t, countErr)
			assert.Zero(t, total, "校验失败不得落库")
			assert.Zero(t, env.inv.calls(), "校验失败不得触发缓存失效")
			assert.Empty(t, env.audit.allNotes(), "校验失败不得写审计")
		})
	}
}

// ---------- §3.4 掩码 / 审计 / 密钥卫生 ----------

func TestLLMProviderAdminCreatePersistsEncryptedKeyAndMaskedDTO(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()

	created := mustCreateProvider(t, env, validCreateRequest("openai-main"))
	assert.Equal(t, "openai-main", created.Key)
	assert.Equal(t, service.LLMProtocolOpenAIChatCompletions, created.Protocol)
	assert.Empty(t, created.Variant)
	assert.Equal(t, "manual", created.Source)
	assert.Equal(t, "configured", created.Status)
	assert.True(t, created.Enabled)
	assert.False(t, created.IsDefault)
	assert.True(t, created.HasAPIKey)

	assert.NotEqual(t, llmAdminPlainKey, created.MaskedAPIKey)
	assert.Contains(t, created.MaskedAPIKey, "****", "MaskSecret ≥16 字节口径")
	assert.NotContains(t, created.MaskedAPIKey, "enc:", "响应不得出现密文前缀")
	assert.True(t, strings.HasPrefix(created.MaskedAPIKey, llmAdminPlainKey[:4]))
	assert.True(t, strings.HasSuffix(created.MaskedAPIKey, llmAdminPlainKey[len(llmAdminPlainKey)-4:]))

	record, err := env.client.LLMProviderConfig.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, env.cipher.prefix+llmAdminPlainKey, record.EncryptedAPIKey)
	assert.NotEqual(t, llmAdminPlainKey, record.EncryptedAPIKey, "落库必须是密文")
	assert.Equal(t, llmAdminTenantID, record.TenantID)

	assert.Equal(t, 1, env.inv.calls())
	assert.Equal(t, []int{llmAdminTenantID}, env.inv.tenants)

	assert.Equal(t, []string{"create"}, env.audit.actions(t))
	for _, note := range env.audit.allNotes() {
		assert.NotContains(t, note, llmAdminPlainKey, "审计 notes 不得含明文密钥")
	}

	// displayName 缺省回退 name。
	fallback := validCreateRequest("fallback-name")
	fallback.DisplayName = ""
	fallbackDTO := mustCreateProvider(t, env, fallback)
	assert.Equal(t, "fallback-name", fallbackDTO.DisplayName)

	list, err := env.svc.ListProviders(ctx, llmAdminTenantID)
	require.NoError(t, err)
	assert.Equal(t, 2, list.Total)
	require.Len(t, list.ProtocolOptions, 4)
	assert.Equal(t, service.LLMProtocolOpenAIChatCompletions, list.ProtocolOptions[0].Protocol)
	assert.True(t, list.ProtocolOptions[0].Implemented)
	// PA-4：4 值枚举全部落地（implemented=true），gemini 只开放标准形态。
	assert.Equal(t, service.LLMProtocolOpenAIResponses, list.ProtocolOptions[2].Protocol)
	assert.True(t, list.ProtocolOptions[2].Implemented)
	assert.Equal(t, []string{""}, list.ProtocolOptions[2].Variants)
	assert.Equal(t, service.LLMProtocolGoogleGemini, list.ProtocolOptions[3].Protocol)
	assert.True(t, list.ProtocolOptions[3].Implemented)
	assert.Equal(t, []string{""}, list.ProtocolOptions[3].Variants)
	for _, option := range list.ProtocolOptions {
		assert.True(t, option.Implemented, "PA-4 收官：%s 必须 implemented=true", option.Protocol)
	}
	for _, item := range list.Items {
		assert.NotContains(t, item.MaskedAPIKey, llmAdminPlainKey)
		assert.NotContains(t, item.MaskedAPIKey, "enc:")
	}
}

func TestLLMProviderAdminCreateNameConflictAndTenantIsolation(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()

	first := mustCreateProvider(t, env, validCreateRequest("dup-key"))
	_, err := env.svc.CreateProvider(ctx, llmAdminTenantID, llmAdminUserID, validCreateRequest("dup-key"))
	requireLLMAdminError(t, err, 409, "AI_PROVIDER_NAME_CONFLICT")

	// name 先 trim 再判重。
	padded := validCreateRequest("dup-key")
	padded.Name = "  dup-key  "
	_, err = env.svc.CreateProvider(ctx, llmAdminTenantID, llmAdminUserID, padded)
	requireLLMAdminError(t, err, 409, "AI_PROVIDER_NAME_CONFLICT")

	// 同名不同租户互不影响。
	other, err := env.svc.CreateProvider(ctx, 77, 99, validCreateRequest("dup-key"))
	require.NoError(t, err)
	assert.Equal(t, "dup-key", other.Key)

	// 跨租户更新/删除/测试一律 404（不泄漏存在性）。
	_, err = env.svc.UpdateProvider(ctx, 77, 99, first.ID, dto.LLMUpdateProviderRequest{})
	requireLLMAdminError(t, err, 404, "AI_PROVIDER_NOT_FOUND")
	_, err = env.svc.DeleteProvider(ctx, 77, 99, first.ID)
	requireLLMAdminError(t, err, 404, "AI_PROVIDER_NOT_FOUND")
	_, err = env.svc.TestProvider(ctx, 77, 99, first.ID)
	requireLLMAdminError(t, err, 404, "AI_PROVIDER_NOT_FOUND")

	tenantOne, err := env.svc.ListProviders(ctx, llmAdminTenantID)
	require.NoError(t, err)
	assert.Equal(t, 1, tenantOne.Total)
	tenantOther, err := env.svc.ListProviders(ctx, 77)
	require.NoError(t, err)
	assert.Equal(t, 1, tenantOther.Total)
}

// ---------- §3.4 更新：部分更新 / 清空语义 / 默认保护 ----------

func TestLLMProviderAdminUpdatePaths(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()
	created := mustCreateProvider(t, env, validCreateRequest("main"))

	updated, err := env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		DisplayName: llmAdminStringPtr("主实例"),
		Model:       llmAdminStringPtr("gpt-4.1-mini"),
		Enabled:     llmAdminBoolPtr(false),
	})
	require.NoError(t, err)
	assert.Equal(t, "主实例", updated.DisplayName)
	assert.Equal(t, "gpt-4.1-mini", updated.Model)
	assert.False(t, updated.Enabled)
	assert.True(t, updated.HasAPIKey, "未传 apiKey 时密钥保持不变")

	// 禁用非默认实例 → 可再设默认时 409 DISABLED。
	_, err = env.svc.SetDefaultProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID)
	requireLLMAdminError(t, err, 409, "AI_PROVIDER_DISABLED")

	// apiKey 传空串 = 清空；密文同步清空。
	cleared, err := env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		APIKey: llmAdminStringPtr(""),
	})
	require.NoError(t, err)
	assert.False(t, cleared.HasAPIKey)
	assert.Empty(t, cleared.MaskedAPIKey)
	record, err := env.client.LLMProviderConfig.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Empty(t, record.EncryptedAPIKey)

	// 重新设置密钥 → 新密文（不与旧值比较，直接校验与明文对应）。
	rotated, err := env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		APIKey: llmAdminStringPtr("sk-rotated-key-abcdefgh"),
	})
	require.NoError(t, err)
	assert.True(t, rotated.HasAPIKey)
	record, err = env.client.LLMProviderConfig.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, env.cipher.prefix+"sk-rotated-key-abcdefgh", record.EncryptedAPIKey)

	// adapterOptions "{}" = 清空。
	withOptions, err := env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		AdapterOptions: json.RawMessage(`{"max_tokens":512}`),
	})
	require.NoError(t, err)
	assert.Equal(t, float64(512), withOptions.AdapterOptions["max_tokens"])
	clearedOptions, err := env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		AdapterOptions: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	assert.Empty(t, clearedOptions.AdapterOptions)

	// PA-4：协议切换到 google_gemini（适配器承载）→ 成功，变体归一为标准形态。
	switchedProtocol, err := env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		Protocol: llmAdminStringPtr(service.LLMProtocolGoogleGemini),
	})
	require.NoError(t, err)
	assert.Equal(t, service.LLMProtocolGoogleGemini, switchedProtocol.Protocol)
	assert.Equal(t, service.LLMVariantDefault, switchedProtocol.Variant)

	// 协议枚举非法 → 422，且原值不变。
	_, err = env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		Protocol: llmAdminStringPtr("openai_completions"),
	})
	requireLLMAdminError(t, err, 422, "AI_PROTOCOL_INVALID")

	// displayName 超长 → 422。
	_, err = env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, created.ID, dto.LLMUpdateProviderRequest{
		DisplayName: llmAdminStringPtr(strings.Repeat("y", 101)),
	})
	requireLLMAdminError(t, err, 422, "AI_PROVIDER_VALIDATION_ERROR")

	// id 非法 / 不存在。
	_, err = env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, 0, dto.LLMUpdateProviderRequest{})
	requireLLMAdminError(t, err, 400, "AI_PROVIDER_VALIDATION_ERROR")
	_, err = env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, 999999, dto.LLMUpdateProviderRequest{})
	requireLLMAdminError(t, err, 404, "AI_PROVIDER_NOT_FOUND")
}

func TestLLMProviderAdminDefaultAndDeleteConstraints(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()

	a := mustCreateProvider(t, env, func() dto.LLMCreateProviderRequest {
		req := validCreateRequest("tenant-a")
		req.IsDefault = true
		return req
	}())
	assert.True(t, a.IsDefault)

	b := mustCreateProvider(t, env, validCreateRequest("tenant-b"))
	assert.False(t, b.IsDefault)

	list, err := env.svc.ListProviders(ctx, llmAdminTenantID)
	require.NoError(t, err)
	require.Len(t, list.Items, 2)
	assert.True(t, list.Items[0].IsDefault, "默认实例排首位")

	// 切换默认：事务「先清后置」，任一时刻仅一个默认。
	switched, err := env.svc.SetDefaultProvider(ctx, llmAdminTenantID, llmAdminUserID, b.ID)
	require.NoError(t, err)
	assert.True(t, switched.IsDefault)
	reloaded, err := env.client.LLMProviderConfig.Get(ctx, a.ID)
	require.NoError(t, err)
	assert.False(t, reloaded.IsDefault)
	defCount := 0
	list, err = env.svc.ListProviders(ctx, llmAdminTenantID)
	require.NoError(t, err)
	for _, item := range list.Items {
		if item.IsDefault {
			defCount++
		}
	}
	assert.Equal(t, 1, defCount)

	// 默认实例禁禁用 / 禁删除。
	_, err = env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, b.ID, dto.LLMUpdateProviderRequest{
		Enabled: llmAdminBoolPtr(false),
	})
	requireLLMAdminError(t, err, 409, "AI_PROVIDER_IS_DEFAULT")
	_, err = env.svc.DeleteProvider(ctx, llmAdminTenantID, llmAdminUserID, b.ID)
	requireLLMAdminError(t, err, 409, "AI_PROVIDER_IS_DEFAULT")
	_, err = env.svc.SetDefaultProvider(ctx, llmAdminTenantID, llmAdminUserID, 999999)
	requireLLMAdminError(t, err, 404, "AI_PROVIDER_NOT_FOUND")

	// 禁用实例不能被设为默认。
	c := mustCreateProvider(t, env, func() dto.LLMCreateProviderRequest {
		req := validCreateRequest("tenant-c")
		req.Enabled = llmAdminBoolPtr(false)
		return req
	}())
	_, err = env.svc.SetDefaultProvider(ctx, llmAdminTenantID, llmAdminUserID, c.ID)
	requireLLMAdminError(t, err, 409, "AI_PROVIDER_DISABLED")

	// 个人默认指向 a 后删除 a：偏好随删除同事务清理。
	pref, err := env.svc.SetUserPreference(ctx, llmAdminTenantID, llmAdminUserID, a.Key)
	require.NoError(t, err)
	assert.Equal(t, a.Key, pref.ProviderKey)
	prefCount, err := env.client.LLMUserPreference.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, prefCount)

	deleted, err := env.svc.DeleteProvider(ctx, llmAdminTenantID, llmAdminUserID, a.ID)
	require.NoError(t, err)
	assert.Equal(t, a.ID, deleted.ID)
	assert.True(t, deleted.Deleted)

	prefCount, err = env.client.LLMUserPreference.Query().Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, prefCount, "删除实例必须清理同租户个人偏好")

	remaining, err := env.svc.ListProviders(ctx, llmAdminTenantID)
	require.NoError(t, err)
	assert.Equal(t, 2, remaining.Total, "软删实例不再出现在列表")
	rawCount, err := env.client.LLMProviderConfig.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, rawCount, "软删保留物理行（name 唯一键占位）")
}

// ---------- §3.3 解析链：user → tenant → static ----------

func TestLLMProviderAdminUserPreferenceResolution(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()

	p1 := mustCreateProvider(t, env, validCreateRequest("pref-p1"))
	p2 := mustCreateProvider(t, env, func() dto.LLMCreateProviderRequest {
		req := validCreateRequest("pref-p2")
		req.IsDefault = true
		return req
	}())

	pref, err := env.svc.GetUserPreference(ctx, llmAdminTenantID, llmAdminUserID)
	require.NoError(t, err)
	assert.Empty(t, pref.ProviderKey)
	assert.Equal(t, p2.Key, pref.EffectiveProviderKey)
	assert.Equal(t, service.ProviderSourceTenant, pref.Source)

	pref, err = env.svc.SetUserPreference(ctx, llmAdminTenantID, llmAdminUserID, p1.Key)
	require.NoError(t, err)
	assert.Equal(t, p1.Key, pref.ProviderKey)
	assert.Equal(t, p1.Key, pref.EffectiveProviderKey)
	assert.Equal(t, service.ProviderSourceUser, pref.Source)

	pref, err = env.svc.GetUserPreference(ctx, llmAdminTenantID, llmAdminUserID)
	require.NoError(t, err)
	assert.Equal(t, service.ProviderSourceUser, pref.Source)
	assert.Equal(t, p1.Key, pref.EffectiveProviderKey)

	// 清除个人默认 → 回退租户默认且物理删除偏好行。
	pref, err = env.svc.SetUserPreference(ctx, llmAdminTenantID, llmAdminUserID, "")
	require.NoError(t, err)
	assert.Empty(t, pref.ProviderKey)
	assert.Equal(t, p2.Key, pref.EffectiveProviderKey)
	assert.Equal(t, service.ProviderSourceTenant, pref.Source)
	prefRows, err := env.client.LLMUserPreference.Query().Count(ctx)
	require.NoError(t, err)
	assert.Zero(t, prefRows)

	// 未命中 / 已禁用实例。
	_, err = env.svc.SetUserPreference(ctx, llmAdminTenantID, llmAdminUserID, "ghost-key")
	requireLLMAdminError(t, err, 404, "AI_PROVIDER_NOT_FOUND")
	_, err = env.svc.UpdateProvider(ctx, llmAdminTenantID, llmAdminUserID, p1.ID, dto.LLMUpdateProviderRequest{
		Enabled: llmAdminBoolPtr(false),
	})
	require.NoError(t, err)
	_, err = env.svc.SetUserPreference(ctx, llmAdminTenantID, llmAdminUserID, p1.Key)
	requireLLMAdminError(t, err, 409, "AI_PROVIDER_DISABLED")

	// 空租户：static 回退（无 providerKey / effectiveProviderKey）。
	empty, err := env.svc.GetUserPreference(ctx, 9001, 9002)
	require.NoError(t, err)
	assert.Empty(t, empty.ProviderKey)
	assert.Empty(t, empty.EffectiveProviderKey)
	assert.Equal(t, service.ProviderSourceStatic, empty.Source)

	actions := env.audit.actions(t)
	assert.Contains(t, actions, "set_user_preference")
	assert.Contains(t, actions, "clear_user_preference")
}

func TestLLMProviderAdminListAvailableFilter(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()

	enabled := mustCreateProvider(t, env, func() dto.LLMCreateProviderRequest {
		req := validCreateRequest("avail-on")
		req.IsDefault = true
		return req
	}())
	mustCreateProvider(t, env, func() dto.LLMCreateProviderRequest {
		req := validCreateRequest("avail-off")
		req.Enabled = llmAdminBoolPtr(false)
		return req
	}())

	items, err := env.svc.ListAvailable(ctx, llmAdminTenantID)
	require.NoError(t, err)
	require.Len(t, items, 1, "available 仅含启用实例")
	assert.Equal(t, enabled.Key, items[0].Key)
	assert.Equal(t, service.LLMProtocolOpenAIChatCompletions, items[0].Protocol)
	assert.True(t, items[0].Implemented)
	assert.True(t, items[0].SupportsStream)
	assert.True(t, items[0].IsDefault)
}

// ---------- §3.4 连通性测试（桩服务 + 脱敏） ----------

func TestLLMProviderAdminTestProviderConnectivity(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()

	var failMode int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.LoadInt32(&failMode) == 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chatcmpl-test","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key ` + llmAdminPlainKey + `","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(server.Close)

	okReq := validCreateRequest("conn-ok")
	okReq.Endpoint = server.URL
	okProvider := mustCreateProvider(t, env, okReq)

	result, err := env.svc.TestProvider(ctx, llmAdminTenantID, llmAdminUserID, okProvider.ID)
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.Equal(t, "ok", result.Status)
	assert.Empty(t, result.Error)
	record, err := env.client.LLMProviderConfig.Get(ctx, okProvider.ID)
	require.NoError(t, err)
	assert.Equal(t, "ok", record.Status)
	assert.Empty(t, record.LastError)
	require.NotNil(t, record.LastTestedAt)

	// 上游 401 且响应体回显明文密钥：返回与落库都必须脱敏。
	atomic.StoreInt32(&failMode, 1)
	failReq := validCreateRequest("conn-fail")
	failReq.Endpoint = server.URL
	failProvider := mustCreateProvider(t, env, failReq)

	result, err = env.svc.TestProvider(ctx, llmAdminTenantID, llmAdminUserID, failProvider.ID)
	require.NoError(t, err, "连通性失败不升级为 handler 错误（ok=false 表达）")
	assert.False(t, result.OK)
	assert.Equal(t, "error", result.Status)
	require.NotEmpty(t, result.Error)
	assert.NotContains(t, result.Error, llmAdminPlainKey)
	record, err = env.client.LLMProviderConfig.Get(ctx, failProvider.ID)
	require.NoError(t, err)
	assert.Equal(t, "error", record.Status)
	assert.NotContains(t, record.LastError, llmAdminPlainKey)
	assert.NotEmpty(t, record.LastError)

	// 未配置密钥 → 422 AI_PROVIDER_KEY_MISSING，且状态落库为 error。
	noKeyReq := validCreateRequest("conn-nokey")
	noKeyReq.APIKey = ""
	noKeyReq.Endpoint = server.URL
	keyless := mustCreateProvider(t, env, noKeyReq)
	_, err = env.svc.TestProvider(ctx, llmAdminTenantID, llmAdminUserID, keyless.ID)
	requireLLMAdminError(t, err, 422, "AI_PROVIDER_KEY_MISSING")
	record, err = env.client.LLMProviderConfig.Get(ctx, keyless.ID)
	require.NoError(t, err)
	assert.Equal(t, "error", record.Status)
	assert.NotContains(t, record.LastError, llmAdminPlainKey)
}

// ---------- §3.4 import-static 幂等 ----------

func TestLLMProviderAdminImportStaticIdempotent(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-static-import-secret-1234")
	env := newLLMAdminTestEnv(t)
	ctx := context.Background()

	first, err := env.svc.ImportStatic(ctx, llmAdminTenantID, llmAdminUserID)
	require.NoError(t, err)
	assert.True(t, first.Created)
	assert.False(t, first.Updated)
	assert.Equal(t, "imported", first.Provider.Source)
	assert.True(t, first.Provider.HasAPIKey)
	assert.NotContains(t, first.Provider.MaskedAPIKey, "sk-static-import-secret-1234")

	second, err := env.svc.ImportStatic(ctx, llmAdminTenantID, llmAdminUserID)
	require.NoError(t, err)
	assert.True(t, second.Updated)
	assert.False(t, second.Created)
	assert.Equal(t, first.Provider.ID, second.Provider.ID)
	assert.Equal(t, first.Provider.Key, second.Provider.Key)

	total, err := env.client.LLMProviderConfig.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, total, "重复导入不得产生第二条记录")
	assert.Contains(t, env.audit.actions(t), "import_static")
	assert.GreaterOrEqual(t, env.inv.calls(), 2)
}

// ---------- §3.4 HTTP 契约（状态码 + errorCode + 掩码） ----------

func newLLMAdminTestEngine(handler *ai.LLMProviderAdminHandler, withIdentity bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	attach := func(c *gin.Context) {
		if withIdentity {
			c.Set("tenant_id", llmAdminTenantID)
			c.Set("user_id", llmAdminUserID)
		}
	}
	engine.GET("/providers", attach, handler.ListProviders)
	engine.POST("/providers", attach, handler.CreateProvider)
	engine.PUT("/providers/:id", attach, handler.UpdateProvider)
	engine.DELETE("/providers/:id", attach, handler.DeleteProvider)
	engine.POST("/providers/:id/default", attach, handler.SetDefaultProvider)
	engine.POST("/providers/:id/test", attach, handler.TestProvider)
	engine.POST("/providers/import-static", attach, handler.ImportStatic)
	engine.GET("/providers/available", attach, handler.ListAvailable)
	engine.GET("/user-preference", attach, handler.GetUserPreference)
	engine.PUT("/user-preference", attach, handler.SetUserPreference)
	return engine
}

func doLLMAdminRequest(engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestLLMProviderAdminHandlerEnvelope(t *testing.T) {
	env := newLLMAdminTestEnv(t)
	handler := ai.NewLLMProviderAdminHandler(env.svc)
	engine := newLLMAdminTestEngine(handler, true)

	// 缺身份 → 401（与既有 AI handler 口径一致）。
	anonymous := doLLMAdminRequest(newLLMAdminTestEngine(handler, false), http.MethodGet, "/providers", "")
	assert.Equal(t, http.StatusUnauthorized, anonymous.Code)

	// 数据面缺失 → 503 + AI_PROVIDER_UNAVAILABLE。
	unavailable := doLLMAdminRequest(newLLMAdminTestEngine(ai.NewLLMProviderAdminHandler(nil), true), http.MethodGet, "/providers", "")
	assert.Equal(t, http.StatusServiceUnavailable, unavailable.Code)
	assert.Contains(t, unavailable.Body.String(), "AI_PROVIDER_UNAVAILABLE")

	// :id 非正整数 → 400；未命中 → 404 + 契约 code。
	badID := doLLMAdminRequest(engine, http.MethodDelete, "/providers/abc", "")
	assert.Equal(t, http.StatusBadRequest, badID.Code)
	missing := doLLMAdminRequest(engine, http.MethodDelete, "/providers/999999", "")
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.Contains(t, missing.Body.String(), "AI_PROVIDER_NOT_FOUND")

	// 校验失败 → 422 + AI_PROVIDER_NAME_INVALID。
	invalidBody := `{"name":"Bad Name","protocol":"openai_chat_completions","model":"gpt-4o-mini","apiKey":"` + llmAdminPlainKey + `"}`
	invalid := doLLMAdminRequest(engine, http.MethodPost, "/providers", invalidBody)
	assert.Equal(t, http.StatusUnprocessableEntity, invalid.Code)
	assert.Contains(t, invalid.Body.String(), "AI_PROVIDER_NAME_INVALID")

	// 创建成功 → 200，响应只暴露掩码且绝无明文/密文。
	validBody := `{"name":"http-main","protocol":"openai_chat_completions","model":"gpt-4o-mini","apiKey":"` + llmAdminPlainKey + `"}`
	created := doLLMAdminRequest(engine, http.MethodPost, "/providers", validBody)
	require.Equal(t, http.StatusOK, created.Code)
	assert.Contains(t, created.Body.String(), "maskedApiKey")
	assert.NotContains(t, created.Body.String(), llmAdminPlainKey)
	assert.NotContains(t, created.Body.String(), "enc:")

	// 重名 → 409 + AI_PROVIDER_NAME_CONFLICT。
	conflict := doLLMAdminRequest(engine, http.MethodPost, "/providers", validBody)
	assert.Equal(t, http.StatusConflict, conflict.Code)
	assert.Contains(t, conflict.Body.String(), "AI_PROVIDER_NAME_CONFLICT")

	// 列表 → 200 + protocolOptions（BE-8 下拉数据源）。
	list := doLLMAdminRequest(engine, http.MethodGet, "/providers", "")
	require.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), "protocolOptions")

	// 可用列表 → 200。
	available := doLLMAdminRequest(engine, http.MethodGet, "/providers/available", "")
	require.Equal(t, http.StatusOK, available.Code)
	assert.Contains(t, available.Body.String(), "http-main")
}
