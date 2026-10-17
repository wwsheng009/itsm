package project

import (
	"context"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/projectgate"
)

// GateCreateParams 创建阶段门参数（PE-12）。
type GateCreateParams struct {
	TenantID  int
	ProjectID int
	Gate      string
	Status    string
}

// GateSubmitPatch 提交补丁（提交人/时间/流程实例/审批链/阻塞明细）。
type GateSubmitPatch struct {
	SubmittedBy       int
	SubmittedAt       time.Time
	ProcessInstanceID int
	ApprovalChainID   string
	Blockers          []map[string]any
	// DecidedBy/DecidedAt/DecisionComment：降级自动通过时随提交一并记录。
	DecidedBy       int
	DecidedAt       *time.Time
	DecisionComment string
}

// GateDecisionPatch 裁决补丁（裁决人/时间/意见/阻塞明细）。
type GateDecisionPatch struct {
	DecidedBy       int
	DecidedAt       time.Time
	DecisionComment string
	Blockers        []map[string]any
}

// ListGates 项目阶段门列表（gate 升序）。
func (r *EntRepository) ListGates(ctx context.Context, tenantID, projectID int) ([]*ent.ProjectGate, error) {
	return r.client.ProjectGate.Query().
		Where(projectgate.TenantIDEQ(tenantID), projectgate.ProjectIDEQ(projectID)).
		Order(ent.Asc(projectgate.FieldGate)).
		All(ctx)
}

// GetGate 取单门（不存在返回 ent.IsNotFound）。
func (r *EntRepository) GetGate(ctx context.Context, tenantID, projectID int, gate string) (*ent.ProjectGate, error) {
	return r.client.ProjectGate.Query().
		Where(
			projectgate.TenantIDEQ(tenantID),
			projectgate.ProjectIDEQ(projectID),
			projectgate.GateEQ(gate),
		).
		Only(ctx)
}

// GetGateByID 按ID取门（租户内）。
func (r *EntRepository) GetGateByID(ctx context.Context, tenantID, id int) (*ent.ProjectGate, error) {
	return r.client.ProjectGate.Query().
		Where(projectgate.TenantIDEQ(tenantID), projectgate.IDEQ(id)).
		Only(ctx)
}

// CreateGate 创建门记录。
func (r *EntRepository) CreateGate(ctx context.Context, p GateCreateParams) (*ent.ProjectGate, error) {
	status := p.Status
	if status == "" {
		status = GateStatusPending
	}
	return r.client.ProjectGate.Create().
		SetTenantID(p.TenantID).
		SetProjectID(p.ProjectID).
		SetGate(p.Gate).
		SetStatus(status).
		Save(ctx)
}

// GateSubmitCAS 提交/重提交 CAS：仅当当前 status/version 匹配时更新，否则返回 CASConflictError。
func (r *EntRepository) GateSubmitCAS(
	ctx context.Context, tenantID, id int, fromStatus string, version int,
	patch GateSubmitPatch, toStatus string,
) (*ent.ProjectGate, error) {
	updated, err := r.client.ProjectGate.Update().
		Where(
			projectgate.IDEQ(id),
			projectgate.TenantIDEQ(tenantID),
			projectgate.StatusEQ(fromStatus),
			projectgate.VersionEQ(version),
		).
		SetStatus(toStatus).
		SetSubmittedBy(patch.SubmittedBy).
		SetSubmittedAt(patch.SubmittedAt).
		SetProcessInstanceID(patch.ProcessInstanceID).
		SetApprovalChainID(patch.ApprovalChainID).
		SetBlockers(nonNilBlockers(patch.Blockers)).
		SetDecidedBy(patch.DecidedBy).
		SetNillableDecidedAt(patch.DecidedAt).
		SetDecisionComment(patch.DecisionComment).
		AddVersion(1).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		current, getErr := r.GetGateByID(ctx, tenantID, id)
		if getErr != nil {
			return nil, getErr
		}
		return nil, &CASConflictError{CurrentVersion: current.Version}
	}
	return r.GetGateByID(ctx, tenantID, id)
}

// GateDecisionCAS 裁决 CAS：仅当当前 status/version 匹配时更新，否则返回 CASConflictError。
func (r *EntRepository) GateDecisionCAS(
	ctx context.Context, tenantID, id int, fromStatus string, version int,
	patch GateDecisionPatch, toStatus string,
) (*ent.ProjectGate, error) {
	upd := r.client.ProjectGate.Update().
		Where(
			projectgate.IDEQ(id),
			projectgate.TenantIDEQ(tenantID),
			projectgate.StatusEQ(fromStatus),
			projectgate.VersionEQ(version),
		).
		SetStatus(toStatus).
		SetDecidedBy(patch.DecidedBy).
		SetDecidedAt(patch.DecidedAt).
		SetDecisionComment(patch.DecisionComment).
		SetBlockers(nonNilBlockers(patch.Blockers)).
		AddVersion(1)
	// PE-24（D-42）：打回累计 +1；与状态置 rejected 为同一条 UPDATE（原子）。
	if toStatus == GateStatusRejected {
		upd = upd.AddRejectCount(1)
	}
	updated, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		current, getErr := r.GetGateByID(ctx, tenantID, id)
		if getErr != nil {
			return nil, getErr
		}
		return nil, &CASConflictError{CurrentVersion: current.Version}
	}
	return r.GetGateByID(ctx, tenantID, id)
}

// nonNilBlockers 保证 JSONB 列写入 JSON 数组（而非 null）。
func nonNilBlockers(items []map[string]any) []map[string]any {
	if items == nil {
		return []map[string]any{}
	}
	return items
}
