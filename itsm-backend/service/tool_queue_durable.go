// B1-06：队列持久化与恢复（复用 tool_invocations 作为持久队列）。
//
// 设计取舍：不为队列新增表 —— 已批准的写工具本身就是「待执行」的持久事实
// （approval_state=approved ∧ status=pending），进程重启后按此条件扫描即可恢复；
// 消费方以**条件状态迁移**（pending → running）抢占任务，保证并发/重复恢复下
// 同一确认单只会被执行一次。
package service

import (
	"context"

	"itsm-backend/common/tenantctx"
	"itsm-backend/ent/toolinvocation"
)

// ErrToolQueueRecoveryOverflow 恢复时队列容量不足（剩余记录留待下轮扫描，不丢数据）。
var ErrToolQueueRecoveryOverflow = ErrToolQueueFull

// RecoveryResult 是一次恢复扫描的结果。
type RecoveryResult struct {
	// Found 命中「已批准但未执行」的记录数。
	Found int
	// Enqueued 成功入队数。
	Enqueued int
	// Skipped 因队列满而留待下轮的数量（调用方可稍后重试）。
	Skipped int
}

// RecoverPending 扫描「已批准但未执行」的确认单并重新入队（B1-06 启动恢复）。
//
// 幂等：入队后由 worker 以条件迁移抢占（claim）；重复扫描不会导致重复执行。
// 跨租户扫描（运维视角），但执行仍按各自的 tenant_id 走既有链路。
func (q *ToolQueue) RecoverPending(ctx context.Context, limit int) (RecoveryResult, error) {
	var result RecoveryResult
	if q == nil || q.client == nil {
		return result, nil
	}
	// R2B 阴影观察（2026-10-03）：启动恢复为跨租户运维扫描（见函数注释），
	// 显式 system 作用域（enforce 前置；此前裸 ctx 会被 fail-closed）。
	ctx = tenantctx.SystemContext(ctx, "tool-queue:recover-pending", "startup recovery scan (cross-tenant, ops scope)")
	if limit <= 0 {
		limit = 200
	}
	rows, err := q.client.ToolInvocation.Query().
		Where(
			toolinvocation.ApprovalStateEQ("approved"),
			toolinvocation.StatusEQ("pending"),
		).
		Order(toolinvocation.ByID()).
		Limit(limit).
		All(ctx)
	if err != nil {
		return result, err
	}
	result.Found = len(rows)
	for _, row := range rows {
		if enqueueErr := q.Enqueue(ToolJob{InvocationID: row.ID, TenantID: row.TenantID}); enqueueErr != nil {
			result.Skipped++
			continue
		}
		result.Enqueued++
	}
	if result.Skipped > 0 {
		return result, ErrToolQueueRecoveryOverflow
	}
	return result, nil
}

// claimForExecution 以条件状态迁移抢占任务：仅当记录仍是
// `approval_state=approved ∧ status=pending` 时置为 `running` 并累加尝试次数。
//
// 返回 false 表示该记录已被其它消费者处理或状态已变化 —— 调用方必须跳过，
// 这是「重复恢复/并发消费不产生双执行」的唯一保证点。
func (q *ToolQueue) claimForExecution(ctx context.Context, invocationID int, tenantID int) (bool, error) {
	affected, err := q.client.ToolInvocation.Update().
		Where(
			toolinvocation.ID(invocationID),
			toolinvocation.TenantID(tenantID),
			toolinvocation.ApprovalStateEQ("approved"),
			toolinvocation.StatusEQ("pending"),
		).
		SetStatus("running").
		AddAttemptCount(1).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
