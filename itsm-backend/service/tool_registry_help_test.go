package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"itsm-backend/ent/botartifact"
	"itsm-backend/ent/enttest"
	"itsm-backend/internal/helpindex"
	"itsm-backend/service/bot"
)

type fakeHelpSemanticSearcher struct {
	ensureCalls int
	results     []helpindex.SearchResult
	err         error
}

func (f *fakeHelpSemanticSearcher) EnsureHelpDocsIndexed(context.Context, int) error {
	f.ensureCalls++
	return f.err
}

func (f *fakeHelpSemanticSearcher) SearchHelpDocs(context.Context, int, string, string, int) ([]helpindex.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

type fakeHelpPublisher struct {
	articleID      int
	createTitle    string
	createBody     string
	createAuthorID int
	publishedID    int
	publisherID    int
	changeLog      string
	createCalls    int
	publishCalls   int
}

func (f *fakeHelpPublisher) CreateHelpDraft(_ context.Context, _, authorID int, title, content string) (int, error) {
	f.createCalls++
	f.createAuthorID = authorID
	f.createTitle = title
	f.createBody = content
	if f.articleID <= 0 {
		f.articleID = 501
	}
	return f.articleID, nil
}

func (f *fakeHelpPublisher) PublishHelpDraft(_ context.Context, _, publisherID, articleID int, changeLog string) error {
	f.publishCalls++
	f.publisherID = publisherID
	f.publishedID = articleID
	f.changeLog = changeLog
	return nil
}

// H0-03 Help Agent 工具执行面：只读、无 DB 依赖、参数缺失 fail-visible。

func execHelp(t *testing.T, reg *ToolRegistry, name string, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, err := reg.Execute(context.Background(), 1, name, args)
	require.NoError(t, err)
	// Execute 可能带元数据包装 → JSON 往返后再断言（对形状保持宽容）。
	buf, err := json.Marshal(raw)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(buf, &payload))
	return payload
}

func TestHelpTools_ExecuteBuiltin(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)

	// help_search：SLA 命中帮助文档。
	search := execHelp(t, reg, "help_search", map[string]interface{}{"q": "SLA", "limit": float64(5)})
	assert.Greater(t, search["count"].(float64), float64(0))
	results, ok := search["results"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, results)

	// help_get_page：页面状态与正文。
	page := execHelp(t, reg, "help_get_page", map[string]interface{}{"pageKey": "ticket.detail"})
	assert.Equal(t, "ok", page["status"])
	assert.Equal(t, "ticket.detail", page["pageKey"])
	assert.NotEmpty(t, page["docs"])

	// help_read_doc：按路径读取全文。
	doc := execHelp(t, reg, "help_read_doc", map[string]interface{}{"path": "docs/help/ai/chat.md"})
	assert.Equal(t, "AI 助手（对话工作区）", doc["title"])
	assert.Contains(t, doc["text"], "确认卡片")

	// help_list_gaps：当前种子文档 100% 覆盖 → gaps 为空（coverage=1.0）。
	gaps := execHelp(t, reg, "help_list_gaps", map[string]interface{}{})
	assert.Equal(t, float64(1), gaps["coverage"].(float64))
	assert.Empty(t, gaps["gaps"])
}

func TestHelpTools_MissingArgsAreVisibleErrors(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)

	_, err := reg.Execute(context.Background(), 1, "help_get_page", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pageKey")

	_, err = reg.Execute(context.Background(), 1, "help_read_doc", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")

	_, err = reg.Execute(context.Background(), 1, "help_read_doc", map[string]interface{}{"path": "docs/help/none.md"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// 空查询 → 空结果（不报错，模型可自行改写查询）。
	res := execHelp(t, reg, "help_search", map[string]interface{}{"q": "   "})
	assert.Equal(t, float64(0), res["count"].(float64))
}

func TestHelpSearch_SemanticMerge(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, nil)
	searcher := &fakeHelpSemanticSearcher{results: []helpindex.SearchResult{{
		Path: "docs/help/tickets/detail.md", Title: "语义命中", Source: "semantic",
	}}}
	reg.SetHelpSemanticSearcher(searcher)

	payload := execHelp(t, reg, "help_search", map[string]interface{}{"q": "zzz-no-lexical-hit"})
	require.Equal(t, 1, searcher.ensureCalls)
	require.Equal(t, float64(1), payload["count"])
	results := payload["results"].([]interface{})
	require.Len(t, results, 1)
	assert.Equal(t, "semantic", results[0].(map[string]interface{})["source"])

	// 语义索引/检索失败 → 词法结果不受影响。
	reg.SetHelpSemanticSearcher(&fakeHelpSemanticSearcher{err: ErrHelpSemanticUnavailable})
	payload = execHelp(t, reg, "help_search", map[string]interface{}{"q": "SLA"})
	assert.Greater(t, payload["count"].(float64), float64(0))
}

// TestHelpCreateDraft_Artifact：H0-04 草稿通道——只落 bot_artifacts（plan）、归属隔离、fail-visible。
func TestHelpCreateDraft_Artifact(t *testing.T) {
	ctx := context.Background()
	dsn := "file:help-draft-" + t.Name() + "?mode=memory&cache=shared&_fk=1"
	client := enttest.Open(t, "sqlite3", dsn)
	t.Cleanup(func() { _ = client.Close() })

	tenantA := client.Tenant.Create().SetCode("help-a").SetName("A").SaveX(ctx)
	tenantB := client.Tenant.Create().SetCode("help-b").SetName("B").SaveX(ctx)
	userA := client.User.Create().
		SetUsername("help-a-user").SetEmail("help-a@example.com").SetName("A User").
		SetPasswordHash("x").SetTenantID(tenantA.ID).SaveX(ctx)

	registry := NewToolRegistry(nil, nil, nil, client)
	registry.SetArtifactStore(bot.NewArtifactStore(client))
	runCtx := WithToolActor(WithToolConversation(ctx, 77), userA.ID)

	res, err := registry.Execute(runCtx, tenantA.ID, "help_create_draft", map[string]interface{}{
		"pageKey":    "ticket.detail",
		"content":    "## 适用场景\n- 草稿内容（不发布）",
		"sourceDocs": []interface{}{"docs/help/tickets/list.md"},
	})
	require.NoError(t, err)
	payload, ok := res.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, bot.ArtifactKindDraft, payload["kind"])
	assert.Equal(t, "帮助文档草稿：工单详情", payload["title"])

	artifactId, _ := payload["artifactId"].(int)
	require.NotZero(t, artifactId)
	artifact, err := client.BotArtifact.Query().Where(botartifact.ID(artifactId)).Only(ctx)
	require.NoError(t, err)
	assert.Equal(t, userA.ID, artifact.OwnerUserID)
	assert.Equal(t, tenantA.ID, artifact.TenantID)
	assert.Contains(t, artifact.ContentJSON, "ticket.detail")
	assert.Contains(t, artifact.EvidenceJSON, "docs/help/tickets/list.md")
	assert.Contains(t, artifact.EvidenceJSON, "\"pageStatus\":\"ok\"")

	// 归属/租户隔离：以租户 B 查询同一产物 → 不存在（ArtifactStore 双条件语义）。
	other, err := client.BotArtifact.Query().
		Where(botartifact.ID(artifactId), botartifact.TenantID(tenantB.ID)).
		Only(ctx)
	require.Error(t, err)
	require.Nil(t, other)

	// 未注入产物存储 → 明确失败（fail-visible）。
	bare := NewToolRegistry(nil, nil, nil, nil)
	_, err = bare.Execute(runCtx, tenantA.ID, "help_create_draft", map[string]interface{}{
		"pageKey": "ticket.detail", "content": "x",
	})
	require.ErrorContains(t, err, "未启用")

	// 参数缺失 → 明确错误。
	_, err = registry.Execute(runCtx, tenantA.ID, "help_create_draft", map[string]interface{}{"pageKey": "x"})
	require.ErrorContains(t, err, "content")
}

// T5-09a：草稿更新 → 提交审阅（落 KB 草稿）→ 发布（只发布关联草稿）。
func TestHelpWriteFlow_UpdateSubmitPublish(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:help-write-"+t.Name()+"?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	tenant := client.Tenant.Create().SetCode("help-write").SetName("Help Write").SaveX(ctx)
	user := client.User.Create().
		SetUsername("help-writer").SetEmail("help-writer@example.com").SetName("Writer").
		SetPasswordHash("x").SetTenantID(tenant.ID).SaveX(ctx)
	other := client.User.Create().
		SetUsername("help-other").SetEmail("help-other@example.com").SetName("Other").
		SetPasswordHash("x").SetTenantID(tenant.ID).SaveX(ctx)

	publisher := &fakeHelpPublisher{}
	registry := NewToolRegistry(nil, nil, nil, client)
	registry.SetArtifactStore(bot.NewArtifactStore(client))
	registry.SetHelpPublisher(publisher)
	runCtx := WithToolActor(WithToolConversation(ctx, 88), user.ID)

	created, err := registry.Execute(runCtx, tenant.ID, "help_create_draft", map[string]interface{}{
		"pageKey": "ticket.detail",
		"content": "## 初稿\n- 旧内容",
	})
	require.NoError(t, err)
	artifactId := created.(map[string]interface{})["artifactId"].(int)

	updated, err := registry.Execute(runCtx, tenant.ID, "help_update_draft", map[string]interface{}{
		"artifactId": artifactId,
		"content":    "## 初稿\n- 新内容\n- 补充步骤",
	})
	require.NoError(t, err)
	updatePayload := updated.(map[string]interface{})
	assert.Equal(t, 2, updatePayload["revision"])
	assert.Equal(t, "draft", updatePayload["status"])

	// 跨用户不可读写。
	_, err = registry.Execute(WithToolActor(ctx, other.ID), tenant.ID, "help_update_draft", map[string]interface{}{
		"artifactId": artifactId, "content": "x",
	})
	require.Error(t, err)

	reviewCtx := WithToolActor(ctx, other.ID)
	submitted, err := registry.Execute(reviewCtx, tenant.ID, "help_submit_review", map[string]interface{}{
		"artifactId": artifactId,
	})
	require.NoError(t, err)
	submitPayload := submitted.(map[string]interface{})
	assert.Equal(t, 501, submitPayload["knowledgeArticleId"])
	assert.Equal(t, "in_review", submitPayload["status"])
	assert.Equal(t, 1, publisher.createCalls)
	assert.Equal(t, user.ID, publisher.createAuthorID, "KB 草稿作者保持原 artifact 归属")
	assert.Equal(t, "帮助文档草稿：工单详情", publisher.createTitle)
	assert.Contains(t, publisher.createBody, "新内容")

	// 审阅中不可再改正文（防止 artifact 与 KB 草稿漂移）。
	_, err = registry.Execute(runCtx, tenant.ID, "help_update_draft", map[string]interface{}{
		"artifactId": artifactId, "content": "x",
	})
	require.Error(t, err)

	published, err := registry.Execute(reviewCtx, tenant.ID, "help_publish", map[string]interface{}{
		"artifactId": artifactId,
		"changeLog":  "T5-09a 审阅通过",
	})
	require.NoError(t, err)
	publishPayload := published.(map[string]interface{})
	assert.Equal(t, "published", publishPayload["status"])
	assert.Equal(t, 501, publisher.publishedID)
	assert.Equal(t, other.ID, publisher.publisherID, "发布人记录审批/操作者")
	assert.Equal(t, "T5-09a 审阅通过", publisher.changeLog)
	assert.Equal(t, 1, publisher.publishCalls)

	// 已发布重放幂等；未提交审阅的草稿不可发布。
	replay, err := registry.Execute(runCtx, tenant.ID, "help_publish", map[string]interface{}{"artifactId": artifactId})
	require.NoError(t, err)
	assert.Equal(t, true, replay.(map[string]interface{})["idempotentReplay"])

	draft2, err := registry.Execute(runCtx, tenant.ID, "help_create_draft", map[string]interface{}{
		"pageKey": "ticket.list", "content": "draft only",
	})
	require.NoError(t, err)
	draft2ID := draft2.(map[string]interface{})["artifactId"].(int)
	_, err = registry.Execute(runCtx, tenant.ID, "help_publish", map[string]interface{}{"artifactId": draft2ID})
	require.ErrorContains(t, err, "尚未提交审阅")

	// 写面未装配时 fail-visible。
	bare := NewToolRegistry(nil, nil, nil, client)
	bare.SetArtifactStore(bot.NewArtifactStore(client))
	_, err = bare.Execute(runCtx, tenant.ID, "help_submit_review", map[string]interface{}{"artifactId": draft2ID})
	require.ErrorContains(t, err, "知识库写面未启用")

}
