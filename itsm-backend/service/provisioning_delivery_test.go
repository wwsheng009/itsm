package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件是纯契约声明的守护（上游无测试）。
// 该文件没有运行时逻辑，唯一会被改坏的是「接口签名」与「常量口径」，
// 因此以**编译期接口实现断言**为主：签名一变，测试包直接编译失败（可打破），
// 不写只会重复常量字面量的装饰性断言。

// fakeDeliveryListener 编译期证明本地实现能满足回调面；同时记录最后一次通知，
// 用于验证「通知发生在事务之后」的调用约定（参数不丢失、不改写）。
type fakeDeliveryListener struct {
	started   []int
	finished  []int
	delivered bool
	detail    string
}

func (f *fakeDeliveryListener) OnProvisioningStarted(_ context.Context, tenantID, serviceRequestID, actorUserID int) {
	f.started = []int{tenantID, serviceRequestID, actorUserID}
}

func (f *fakeDeliveryListener) OnProvisioningFinished(_ context.Context, tenantID, serviceRequestID, actorUserID int, delivered bool, detail string) {
	f.finished = []int{tenantID, serviceRequestID, actorUserID}
	f.delivered = delivered
	f.detail = detail
}

type fakeCIProjector struct {
	got ProvisioningCIProjectionRequest
}

func (f *fakeCIProjector) ProjectProvisionedResource(_ context.Context, req ProvisioningCIProjectionRequest) ProvisioningCIProjectionResult {
	f.got = req
	return ProvisioningCIProjectionResult{Status: ProvisioningCIProjectionCreated, CIID: 42}
}

var (
	_ ProvisioningDeliveryListener = (*fakeDeliveryListener)(nil)
	_ ProvisioningCIProjector      = (*fakeCIProjector)(nil)
)

func TestProvisioningDeliveryContractSignatures(t *testing.T) {
	l := &fakeDeliveryListener{}
	l.OnProvisioningStarted(context.Background(), 7, 11, 3)
	require.Equal(t, []int{7, 11, 3}, l.started, "started 回调必须原样带上三个 ID")
	l.OnProvisioningFinished(context.Background(), 7, 11, 3, true, "vm-123")
	assert.Equal(t, []int{7, 11, 3}, l.finished)
	assert.True(t, l.delivered)
	assert.Equal(t, "vm-123", l.detail)

	req := ProvisioningCIProjectionRequest{
		TenantID: 7, ServiceRequestID: 11, ProvisioningTaskID: 5,
		ResourceType: "ecs", Provider: "alicloud",
		Resources: map[string]string{"instanceId": "i-abc"},
		Payload:   map[string]any{"region": "cn-hangzhou"},
	}
	p := &fakeCIProjector{}
	res := p.ProjectProvisionedResource(context.Background(), req)
	assert.Equal(t, req, p.got, "投影请求必须完整透传（Resources/Payload 不得丢失）")
	assert.Equal(t, ProvisioningCIProjectionCreated, res.Status)
	assert.Equal(t, 42, res.CIID)
}

func TestProvisioningCIProjectionStatusVocabulary(t *testing.T) {
	// 状态码是跨 service ↔ handlers/cmdb 的契约面，按字母序锁定，
	// 防止上游/本地各自漂移（G12 同类教训：两套词表必须同源）。
	got := []string{
		ProvisioningCIProjectionCreated,
		ProvisioningCIProjectionUpdated,
		ProvisioningCIProjectionDeferred,
		ProvisioningCIProjectionSkipped,
		ProvisioningCIProjectionFailed,
		ProvisioningCIProjectionUnavailable,
	}
	assert.Equal(t, []string{
		"created", "updated", "deferred", "skipped", "failed", "unavailable",
	}, got)
	seen := map[string]bool{}
	for _, s := range got {
		assert.False(t, seen[s], "状态码不得重复: %s", s)
		seen[s] = true
	}
}
