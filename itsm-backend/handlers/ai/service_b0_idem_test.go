package ai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"itsm-backend/ent"
	"itsm-backend/handlers/ai"
	"itsm-backend/service"
	"itsm-backend/service/bot"
)

// b0IdemRepo 在 rbacMockRepo 之上补「幂等键索引」与「并发冲突模拟」，
// 用于 B0-05 的幂等命中/回放断言（不依赖真实 DB）。
//
// raceFirstLookup=true 模拟并发语义：预查未命中（另一请求尚未提交），
// 创建时唯一索引冲突，随后回查命中既有记录。
type b0IdemRepo struct {
	*rbacMockRepo
	records         []*ai.ToolInvocation
	raceFirstLookup bool
	lookups         int
	creates         int
}

func (m *b0IdemRepo) GetToolInvocationByIdempotencyKey(_ context.Context, tenantID int, keyHash string) (*ai.ToolInvocation, error) {
	m.lookups++
	if keyHash == "" {
		return nil, nil
	}
	if m.raceFirstLookup && m.lookups == 1 {
		return nil, nil
	}
	for _, r := range m.records {
		if r.TenantID == tenantID && r.IdempotencyKeyHash == keyHash {
			return r, nil
		}
	}
	return nil, nil
}

func (m *b0IdemRepo) CreateToolInvocation(_ context.Context, i *ai.ToolInvocation) (*ai.ToolInvocation, error) {
	m.creates++
	if i.IdempotencyKeyHash != "" {
		for _, r := range m.records {
			if r.TenantID == i.TenantID && r.IdempotencyKeyHash == i.IdempotencyKeyHash {
				return nil, errors.New("ent: constraint failed: UNIQUE constraint failed: tool_invocations.tenant_id, tool_invocations.idempotency_key_hash")
			}
		}
	}
	i.ID = len(m.records) + 1
	m.records = append(m.records, i)
	return i, nil
}

func newB0IdemEnv(t *testing.T) (*ai.Service, *b0IdemRepo) {
	t.Helper()
	repo := &b0IdemRepo{rbacMockRepo: &rbacMockRepo{}}
	tools := service.NewToolRegistry(nil, nil, nil, nil)
	svc := ai.NewService(repo, zap.NewNop().Sugar(), nil, tools, nil, nil, nil, nil, nil, nil, nil)
	svc.SetEntClient(&ent.Client{})
	return svc, repo
}

// keyFor 用生产代码同源算法计算幂等键（冻结口径：服务端与测试不得各写一套）。
func keyFor(t *testing.T, tenantID, userID int, tool string, args map[string]interface{}) string {
	t.Helper()
	targetType, targetID := bot.TargetFromArgs(args)
	key, err := bot.BuildKey(bot.KeyInput{
		TenantID: tenantID, UserID: userID, Tool: tool,
		TargetType: targetType, TargetID: targetID, Args: args,
	})
	require.NoError(t, err)
	return key
}

// B0-05：顺序重复提交 → 命中既有记录（返回首次结果），不新增记录、不重复进审批队列。
func TestB0_05_SequentialDuplicateReplaysFirstRecord(t *testing.T) {
	svc, repo := newB0IdemEnv(t)
	args := map[string]interface{}{"title": "打印机故障", "priority": "high"}
	key := keyFor(t, 10, 1, "create_ticket", args)
	firstResult := `{"ticket":{"id":99,"status":"pending"}}`
	repo.records = append(repo.records, &ai.ToolInvocation{
		ID: 7, TenantID: 10, ToolName: "create_ticket",
		IdempotencyKeyHash: key, Status: "pending", ApprovalState: "pending",
		Result: &firstResult,
	})

	res, invID, err := svc.ExecuteToolWithOptions(context.Background(), 1, 10, "super_admin", "create_ticket", args, ai.ExecuteToolOptions{})
	require.NoError(t, err)
	assert.Equal(t, 7, invID, "必须返回首次调用记录 ID")
	assert.Equal(t, 0, repo.creates, "命中即回放，不得新增记录")

	payload, ok := res.(map[string]interface{})
	require.True(t, ok, "回放载荷应为结构化 map，实际 %T", res)
	assert.Equal(t, true, payload["idempotentReplay"])
	assert.Equal(t, 7, payload["invocationId"])
	assert.Equal(t, "pending", payload["status"])
	assert.Equal(t, "pending", payload["approvalState"])
	decoded, ok := payload["result"].(map[string]interface{})
	require.True(t, ok, "既有结果应被解码回放")
	assert.Contains(t, decoded, "ticket")
}

// B0-05：参数变化 → 键变化 → 生成新记录（幂等不误伤正常重提）。
func TestB0_05_ParamChangeCreatesNewRecord(t *testing.T) {
	svc, repo := newB0IdemEnv(t)

	_, id1, err := svc.ExecuteToolWithOptions(context.Background(), 1, 10, "super_admin", "create_ticket",
		map[string]interface{}{"title": "A"}, ai.ExecuteToolOptions{})
	require.NoError(t, err)
	_, id2, err := svc.ExecuteToolWithOptions(context.Background(), 1, 10, "super_admin", "create_ticket",
		map[string]interface{}{"title": "B"}, ai.ExecuteToolOptions{})
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2)
	require.Len(t, repo.records, 2)
	assert.NotEmpty(t, repo.records[0].IdempotencyKeyHash, "写工具必须落幂等键 hash")
	assert.NotEqual(t, repo.records[0].IdempotencyKeyHash, repo.records[1].IdempotencyKeyHash)
}

// B0-05：跨租户/跨发起人不串键（作用域包含租户与用户）。
func TestB0_05_ScopeIsolationAcrossTenantAndUser(t *testing.T) {
	svc, repo := newB0IdemEnv(t)
	args := map[string]interface{}{"title": "同题"}

	_, _, err := svc.ExecuteToolWithOptions(context.Background(), 1, 10, "super_admin", "create_ticket", args, ai.ExecuteToolOptions{})
	require.NoError(t, err)
	_, _, err = svc.ExecuteToolWithOptions(context.Background(), 1, 11, "super_admin", "create_ticket", args, ai.ExecuteToolOptions{})
	require.NoError(t, err)
	_, _, err = svc.ExecuteToolWithOptions(context.Background(), 2, 10, "super_admin", "create_ticket", args, ai.ExecuteToolOptions{})
	require.NoError(t, err)

	require.Len(t, repo.records, 3, "跨租户/跨用户必须各自建记录")
	hashes := map[string]bool{}
	for _, r := range repo.records {
		hashes[r.IdempotencyKeyHash] = true
	}
	assert.Len(t, hashes, 3)
}

// B0-05：并发窗口——预查未命中但创建时唯一索引冲突 → 回查命中并回放（非 500）。
func TestB0_05_UniqueViolationRaceReplaysExisting(t *testing.T) {
	svc, repo := newB0IdemEnv(t)
	repo.raceFirstLookup = true
	args := map[string]interface{}{"title": "并发提交"}
	key := keyFor(t, 10, 1, "create_ticket", args)
	repo.records = append(repo.records, &ai.ToolInvocation{
		ID: 5, TenantID: 10, ToolName: "create_ticket",
		IdempotencyKeyHash: key, Status: "pending", ApprovalState: "pending",
	})

	res, invID, err := svc.ExecuteToolWithOptions(context.Background(), 1, 10, "super_admin", "create_ticket", args, ai.ExecuteToolOptions{})
	require.NoError(t, err, "唯一冲突必须映射为幂等命中，而非报错")
	assert.Equal(t, 5, invID)
	assert.Equal(t, 1, repo.creates, "尝试创建一次（冲突后回放，不重试写入）")
	assert.Len(t, repo.records, 1, "不得产生重复记录")

	payload, ok := res.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, payload["idempotentReplay"])
}
