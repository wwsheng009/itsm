package project

import (
	"context"
	"errors"
	"fmt"

	"itsm-backend/ent"
)

// 单据关联哨兵错误（PE-5）。
var (
	ErrRecordNotFound               = errors.New("record not found")
	ErrRecordDomainInvalid          = errors.New("invalid record domain")
	ErrProjectWriteRequired         = errors.New("project write permission required")
	ErrTaskTicketExists             = errors.New("task already has a linked ticket")
	ErrTaskTicketNotLinked          = errors.New("task has no linked ticket")
	ErrTicketGeneratorNotConfigured = errors.New("ticket generator not configured")
)

// SetTicketGenerator 注入工单生成适配器（bootstrap 装配；未注入时生成端点 503）。
func (s *Service) SetTicketGenerator(gen TicketGenerator) {
	s.ticketGen = gen
}

// AttachRecord 挂接单据到项目（契约 §3.1：域写码由路由声明，project:write 在此动态复核）。
func (s *Service) AttachRecord(ctx context.Context, tenantID int, domain string, recordID, projectID int, roles []string) error {
	if !validRecordDomains[domain] {
		return ErrRecordDomainInvalid
	}
	if !s.repo.AuthorizeProjectWrite(ctx, roles, tenantID) {
		return ErrProjectWriteRequired
	}
	current, err := s.Get(ctx, tenantID, projectID)
	if err != nil {
		return err
	}
	if current.Status == StatusCompleted || current.Status == StatusCancelled {
		return fmt.Errorf("%w: project is closed", ErrValidation)
	}
	if err := s.repo.AttachRecord(ctx, tenantID, domain, recordID, projectID); err != nil {
		if ent.IsNotFound(err) {
			return ErrRecordNotFound
		}
		return err
	}
	return nil
}

// DetachRecord 解除挂接（同样要求 project:write）。
func (s *Service) DetachRecord(ctx context.Context, tenantID int, domain string, recordID int, roles []string) error {
	if !validRecordDomains[domain] {
		return ErrRecordDomainInvalid
	}
	if !s.repo.AuthorizeProjectWrite(ctx, roles, tenantID) {
		return ErrProjectWriteRequired
	}
	if err := s.repo.DetachRecord(ctx, tenantID, domain, recordID); err != nil {
		if ent.IsNotFound(err) {
			return ErrRecordNotFound
		}
		return err
	}
	return nil
}

// ListRecords 项目关联单据聚合读（可见性谓词复用 GetForViewer）。
func (s *Service) ListRecords(ctx context.Context, tenantID, projectID, viewerID int, admin bool, domain, status string, offset, limit int) ([]RecordItem, int64, error) {
	if _, err := s.GetForViewer(ctx, tenantID, projectID, viewerID, admin); err != nil {
		return nil, 0, err
	}
	if domain != "" && !validRecordDomains[domain] {
		return nil, 0, ErrRecordDomainInvalid
	}
	return s.repo.ListRecords(ctx, tenantID, projectID, domain, status, offset, limit)
}

// GenerateTaskTicket 任务→工单生成（契约 §3.2：一任务一主工单；幂等拒绝重复）。
func (s *Service) GenerateTaskTicket(ctx context.Context, tenantID, projectID, taskID, actorID int, req *TaskTicketRequest) (*ent.ProjectTask, error) {
	if s.ticketGen == nil {
		return nil, ErrTicketGeneratorNotConfigured
	}
	if req == nil {
		req = &TaskTicketRequest{}
	}
	task, err := s.repo.GetTask(ctx, tenantID, projectID, taskID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	if task.LinkedTicketID > 0 {
		return nil, ErrTaskTicketExists
	}
	ticketID, err := s.ticketGen.GenerateTicket(ctx, TicketGenerateInput{
		TenantID:  tenantID,
		ProjectID: projectID,
		ActorID:   actorID,
		Task:      task,
		Request:   req,
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetTaskLinkedTicket(ctx, tenantID, projectID, taskID, ticketID); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	updated, err := s.repo.GetTask(ctx, tenantID, projectID, taskID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// UnbindTaskTicket 解绑任务主工单（解绑后可重建）。
func (s *Service) UnbindTaskTicket(ctx context.Context, tenantID, projectID, taskID int) (*ent.ProjectTask, error) {
	task, err := s.repo.GetTask(ctx, tenantID, projectID, taskID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	if task.LinkedTicketID <= 0 {
		return nil, ErrTaskTicketNotLinked
	}
	if err := s.repo.ClearTaskLinkedTicket(ctx, tenantID, projectID, taskID); err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return s.repo.GetTask(ctx, tenantID, projectID, taskID)
}

// AttachGeneratedTicket 适配器内部用：把生成的工单挂到项目（同租户校验由仓储完成；
// 任务主工单链接由 GenerateTaskTicket 在适配器返回后统一回写）。
func (s *Service) AttachGeneratedTicket(ctx context.Context, tenantID, ticketID, projectID int) error {
	if err := s.repo.AttachRecord(ctx, tenantID, "ticket", ticketID, projectID); err != nil {
		if ent.IsNotFound(err) {
			return ErrRecordNotFound
		}
		return err
	}
	return nil
}
