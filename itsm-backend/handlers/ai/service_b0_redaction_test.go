package ai_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/handlers/ai"
	"itsm-backend/pkg/redact"
	"itsm-backend/service"
)

// newB0RedactionEnv 构造带注册表的测试环境（B0-06 需要按工具元数据解析脱敏档）。
func newB0RedactionEnv(t *testing.T) (*ai.Service, *b0IdemRepo, *service.ToolRegistry) {
	t.Helper()
	repo := &b0IdemRepo{rbacMockRepo: &rbacMockRepo{}}
	tools := service.NewToolRegistry(nil, nil, nil, nil)
	svc := ai.NewService(repo, zap.NewNop().Sugar(), nil, tools, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(&ent.Client{})
	return svc, repo, tools
}

func lastRecord(t *testing.T, repo *b0IdemRepo) *ai.ToolInvocation {
	t.Helper()
	require.NotEmpty(t, repo.records)
	return repo.records[len(repo.records)-1]
}

// B0-06：写工具待审批记录落库前完成脱敏——明文密钥/口令不得进入 args_redacted。
func TestB0_06_PendingAuditArgsAreRedacted(t *testing.T) {
	svc, repo, _ := newB0RedactionEnv(t)

	args := map[string]interface{}{
		"title":    "打印机故障",
		"password": "P@ssw0rd-明文",
		"nested":   map[string]interface{}{"api_key": "sk-live-abcdef"},
	}
	_, invID, err := svc.ExecuteToolWithOptions(context.Background(), 1, 10, "super_admin", "create_ticket", args, ai.ExecuteToolOptions{})
	require.NoError(t, err)
	require.Positive(t, invID)

	inv := lastRecord(t, repo)
	require.Equal(t, "pending", inv.Status)
	for _, leaked := range []string{"P@ssw0rd-明文", "sk-live-abcdef"} {
		assert.NotContains(t, inv.ArgsRedacted, leaked, "明文不得落库：%s", leaked)
	}
	assert.Contains(t, inv.ArgsRedacted, redact.Mask)
	assert.Contains(t, inv.ArgsRedacted, "打印机故障", "常规字段保留（default 档）")

	// 落库字段是合法 JSON（审计页可直接展示）。
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(inv.ArgsRedacted), &decoded))

	// 显式断言：仅 args_redacted 参与审计展示；真源 Arguments 只服务审批重放，不用于展示。
	assert.NotEmpty(t, inv.Arguments, "执行真源仍在（审批重放依赖）")
}

// B0-06：strict 档工具（内置写工具）——全参数掩码（只留键名）。
func TestB0_06_StrictProfileMasksAllValues(t *testing.T) {
	svc, repo, tools := newB0RedactionEnv(t)

	// 取一个真实标注为 strict 的内置写工具（不硬编码工具名，随冻结矩阵演进）。
	strictTool := ""
	for _, td := range tools.ListTools() {
		if td.RedactionProfile == service.ToolRedactionStrict && !td.ReadOnly {
			strictTool = td.Name
			break
		}
	}
	require.NotEmpty(t, strictTool, "必须存在 strict 档内置写工具（B0-01 冻结矩阵）")

	_, invID, err := svc.ExecuteToolWithOptions(context.Background(), 1, 10, "super_admin", strictTool,
		map[string]interface{}{"ticket_id": float64(1), "ci_id": float64(2), "note": "敏感备注原文"}, ai.ExecuteToolOptions{})
	require.NoError(t, err)
	require.Positive(t, invID)

	inv := lastRecord(t, repo)
	assert.NotContains(t, inv.ArgsRedacted, "敏感备注原文", "strict 档不得落任何值")
	assert.NotContains(t, inv.ArgsRedacted, `"note":"`)
	assert.Contains(t, inv.ArgsRedacted, `"masked":true`)
	assert.True(t, strings.Contains(inv.ArgsRedacted, "keys"), "键名保留，便于审计定位字段")
}
