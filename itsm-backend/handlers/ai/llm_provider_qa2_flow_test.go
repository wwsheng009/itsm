package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
)

// QA-2 端到端验收（API 级）：主计划《多 LLM Provider 支持与可切换方案》§6.2 场景 1-6/10
// （另补充删除 / 禁用后的显式引用失败路径）。
//
// 环境说明（重要）：本用例**不连生产库、不连真实 LLM**——用内存 SQLite（enttest）承载
// llm_provider_configs / llm_user_preferences，用 httptest 桩模拟 OpenAI 兼容端点。
// 与既有单测的区别：这里把「管理 API 写入 → 缓存失效 → 运行期解析 → 真实 HTTP 出站」
// 串成一条链，验证运维动作**当场生效**（而不是各层各自正确但装配断线）。
//
// 覆盖映射（按主计划 §6.2 场景编号）：
//   - 场景 1/2：创建默认实例 A + 连通性测试 → 租户默认即刻生效（providerSource=tenant）；
//   - 场景 3：新建实例 B 后以请求级覆盖切换 → B 生效、静态端点零调用；
//   - 场景 4：切换租户默认（SetDefaultProvider）→ 下一次请求即走新实例；
//   - 场景 5：个人默认（user → tenant 解析链）与实例禁用后的降级（A/B 角色互换，语义等价）；
//   - 场景 6：无实例租户 → 静态 config 回退（providerSource=static）；
//   - 场景 10：openai_chat_completions 协议槽位经真实 adapter 出站（res.Protocol 回带）；
//   - 补充：删除 / 禁用后的显式引用可见地失败（AI_PROVIDER_NOT_FOUND / AI_PROVIDER_DISABLED）。
// 场景 9（幂等导入）由 TestLLMProviderAdminImportStaticIdempotent 覆盖；
// 浏览器级视觉复核（页签显隐/选择器交互）与真实 LLM 联调仍属人工步骤，
// 见 docs/testing/multi-llm-provider-qa2-e2e-2026-09-24.md。

// qa2LLMStub OpenAI 兼容桩：固定正文 + 调用计数（证明路由真实落到对应端点）。
type qa2LLMStub struct {
	server   *httptest.Server
	calls    int32
	lastPath atomic.Value // string
}

func (s *qa2LLMStub) callCount() int32 { return atomic.LoadInt32(&s.calls) }

func (s *qa2LLMStub) lastRequestPath() string {
	if v, ok := s.lastPath.Load().(string); ok {
		return v
	}
	return ""
}

func newQA2LLMStub(t *testing.T, content string) *qa2LLMStub {
	t.Helper()
	stub := &qa2LLMStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&stub.calls, 1)
		stub.lastPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-qa2",
			"object": "chat.completion",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

type qa2FlowEnv struct {
	admin    *ai.LLMProviderAdminService
	registry *service.LLMProviderRegistry
	gateway  *service.LLMGateway
	client   *ent.Client
}

// newQA2FlowEnv 组装生产同构接线：registry 同时作为 admin 的 Invalidator（写后即刻失效），
// gateway.WithResolver(registry) 接上 §3.3 解析链（含个人默认自动接线）。
func newQA2FlowEnv(t *testing.T, staticCfg service.ProviderConfig) *qa2FlowEnv {
	t.Helper()
	client := enttest.Open(t, "sqlite3", llmAdminTestDSN())
	t.Cleanup(func() { _ = client.Close() })

	cipher := llmAdminTestCipher{prefix: "enc:"}
	logger := zap.NewNop().Sugar()
	registry := service.NewLLMProviderRegistry(client, cipher, staticCfg, logger)
	gateway := service.NewLLMGateway(service.NewProviderFromConfig(staticCfg), nil, service.NoopObserver{}, staticCfg.Provider)
	gateway.WithResolver(registry)

	return &qa2FlowEnv{
		admin: ai.NewLLMProviderAdminService(ai.LLMProviderAdminDeps{
			Client:      client,
			Encrypter:   cipher,
			Invalidator: registry,
			Logger:      logger,
			Audit:       &llmAdminTestAudit{},
		}),
		registry: registry,
		gateway:  gateway,
		client:   client,
	}
}

func qa2Chat(t *testing.T, gateway *service.LLMGateway, req service.ProviderRequest) (string, service.ProviderResolution, error) {
	t.Helper()
	return gateway.ChatWithRequest(context.Background(), req, "gpt-4o-mini",
		[]service.LLMMessage{{Role: "user", Content: "ping"}})
}

func TestLLMProviderQA2RuntimeSwitchFlow(t *testing.T) {
	stubA := newQA2LLMStub(t, "PONG-A")
	stubB := newQA2LLMStub(t, "PONG-B")
	stubStatic := newQA2LLMStub(t, "PONG-STATIC")

	staticCfg := service.ProviderConfig{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		APIKey:   "static-key",
		Endpoint: stubStatic.server.URL + "/v1",
	}
	env := newQA2FlowEnv(t, staticCfg)
	ctx := context.Background()
	const tenant = llmAdminTenantID

	// ---- 场景 1/2：创建默认实例 A + 连通性测试 ----
	reqA := validCreateRequest("qa2-a")
	reqA.Endpoint = stubA.server.URL + "/v1"
	reqA.IsDefault = true
	createdA, err := env.admin.CreateProvider(ctx, tenant, llmAdminUserID, reqA)
	require.NoError(t, err)
	assert.True(t, createdA.IsDefault, "首个默认实例应落 is_default=true")
	assert.True(t, createdA.Enabled)
	assert.Equal(t, service.LLMProtocolOpenAIChatCompletions, createdA.Protocol)

	testResp, err := env.admin.TestProvider(ctx, tenant, llmAdminUserID, createdA.ID)
	require.NoError(t, err)
	assert.True(t, testResp.OK, "连通性测试应命中桩服务并返回 ok")
	assert.Equal(t, "ok", testResp.Status)
	require.GreaterOrEqual(t, stubA.callCount(), int32(1), "测试请求必须真实出站到 A 桩")

	// 写入后无需重启/等待 TTL：Invalidator 已让注册表失效。
	out, res, err := qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant})
	require.NoError(t, err)
	assert.Equal(t, "PONG-A", out)
	assert.Equal(t, service.ProviderSourceTenant, res.Source)
	assert.Equal(t, "qa2-a", res.Key)
	assert.Equal(t, service.LLMProtocolOpenAIChatCompletions, res.Protocol, "协议槽位经 openai_chat_completions adapter 出站")
	assert.True(t, strings.HasSuffix(stubA.lastRequestPath(), "/chat/completions"),
		"实例出站请求必须落到 OpenAI 兼容端点: %s", stubA.lastRequestPath())

	// ---- 场景 3：新建实例 B，请求级覆盖立即生效；静态端点零调用 ----
	reqB := validCreateRequest("qa2-b")
	reqB.Endpoint = stubB.server.URL + "/v1"
	createdB, err := env.admin.CreateProvider(ctx, tenant, llmAdminUserID, reqB)
	require.NoError(t, err)

	out, res, err = qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant, Key: "qa2-b"})
	require.NoError(t, err)
	assert.Equal(t, "PONG-B", out)
	assert.Equal(t, service.ProviderSourceRequest, res.Source)
	assert.Equal(t, "qa2-b", res.Key)
	assert.Equal(t, int32(0), stubStatic.callCount(), "显式覆盖不得回落到静态配置")

	// ---- 场景 4：切换租户默认 → 下一次无覆盖请求即走 B ----
	switched, err := env.admin.SetDefaultProvider(ctx, tenant, llmAdminUserID, createdB.ID)
	require.NoError(t, err)
	assert.True(t, switched.IsDefault)

	out, res, err = qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant})
	require.NoError(t, err)
	assert.Equal(t, "PONG-B", out, "切换默认后旧默认实例不得再被选中")
	assert.Equal(t, service.ProviderSourceTenant, res.Source)
	assert.Equal(t, "qa2-b", res.Key)

	// ---- 场景 5a：个人默认（解析链第二级）优先于租户默认 ----
	pref, err := env.admin.SetUserPreference(ctx, tenant, 7, "qa2-a")
	require.NoError(t, err)
	assert.Equal(t, "qa2-a", pref.ProviderKey)

	out, res, err = qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant, UserID: 7})
	require.NoError(t, err)
	assert.Equal(t, "PONG-A", out)
	assert.Equal(t, service.ProviderSourceUser, res.Source)
	assert.Equal(t, "qa2-a", res.Key)

	// ---- 场景 5b：禁用非默认实例 A → 显式引用可见地失败；个人默认降级到租户默认 ----
	_, err = env.admin.UpdateProvider(ctx, tenant, llmAdminUserID, createdA.ID, dto.LLMUpdateProviderRequest{
		Enabled: llmAdminBoolPtr(false),
	})
	require.NoError(t, err)

	_, res, err = qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant, Key: "qa2-a"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrProviderDisabled), "禁用实例的显式引用必须可见失败: %v", err)
	assert.Equal(t, service.ProviderSourceRequest, res.Source, "失败路径仍需回带请求级来源标注")

	out, res, err = qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant, UserID: 7})
	require.NoError(t, err)
	assert.Equal(t, "PONG-B", out, "个人默认指向禁用实例时降级到租户默认")
	assert.Equal(t, service.ProviderSourceTenant, res.Source)

	// ---- 场景 9：删除实例 → 显式引用不可见（NOT_FOUND，不静默回退） ----
	_, err = env.admin.DeleteProvider(ctx, tenant, llmAdminUserID, createdA.ID)
	require.NoError(t, err)

	_, res, err = qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant, Key: "qa2-a"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrProviderNotFound), "已删除实例的显式引用必须 404 语义: %v", err)
	assert.Equal(t, "qa2-a", res.Key)

	// ---- 场景 6：无实例租户 → 静态 config 回退（providerSource=static） ----
	out, res, err = qa2Chat(t, env.gateway, service.ProviderRequest{TenantID: tenant + 1})
	require.NoError(t, err)
	assert.Equal(t, "PONG-STATIC", out)
	assert.Equal(t, service.ProviderSourceStatic, res.Source)
	assert.Equal(t, "openai", res.Key, "静态回退 key 取静态 provider 名")
	require.GreaterOrEqual(t, stubStatic.callCount(), int32(1))
}
