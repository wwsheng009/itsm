package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"itsm-backend/common"
	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/pkg/redact"
	ticketrepo "itsm-backend/repository/ticket"

	"go.uber.org/zap"
)

type ToolJob struct {
	InvocationID int
	TenantID     int
	RequestID    string
}

type ToolQueue struct {
	jobs        chan ToolJob
	client      *ent.Client
	tools       *ToolRegistry
	tickets     *TicketService
	ticketTypes *TicketTypeService
	logger      *zap.SugaredLogger
	// B1-06：执行后回读校验器（未注入时终态 verify_state=skipped）。
	verifier ToolVerifier
}

// ErrToolQueueFull 队列已满（fail-closed：调用方必须把审批留在 pending 可重试，不得静默丢弃）。
var ErrToolQueueFull = errors.New("tool queue is full")

func NewToolQueue(client *ent.Client, tools *ToolRegistry, capacity int, logger *zap.SugaredLogger) *ToolQueue {
	if capacity <= 0 {
		capacity = 100
	}
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	q := &ToolQueue{jobs: make(chan ToolJob, capacity), client: client, tools: tools, logger: logger}
	go q.worker()
	return q
}

// SetTicketTypeService 注入工单类型服务，供 create_ticket_type 审批通过后执行。
func (q *ToolQueue) SetTicketTypeService(s *TicketTypeService) { q.ticketTypes = s }

// SetVerifier 注入执行后回读校验器（B1-06）；未注入时终态落 verify_state=skipped。
func (q *ToolQueue) SetVerifier(v ToolVerifier) { q.verifier = v }

// Enqueue 非阻塞入队；队列满时返回 ErrToolQueueFull（fail-closed）。
//
// 历史行为是「满则静默丢弃」，会让已批准的写工具永久停在 approved 且永不执行——
// 审批人以为已放行、审计里却没有执行结果，故 M1-02 改为显式失败由调用方回滚/提示重试。
func (q *ToolQueue) Enqueue(job ToolJob) error {
	select {
	case q.jobs <- job:
		return nil
	default:
		return ErrToolQueueFull
	}
}

func (q *ToolQueue) worker() {
	for job := range q.jobs {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		startedAt := time.Now()
		inv, err := q.client.ToolInvocation.Get(ctx, job.InvocationID)
		if err != nil {
			cancel()
			continue
		}
		// B1-06：以条件状态迁移抢占任务（pending → running + attempt_count+1）。
		// 重复恢复/并发消费时只有一个消费者能抢占成功，避免写工具被双执行。
		claimed, claimErr := q.claimForExecution(ctx, inv.ID, inv.TenantID)
		if claimErr != nil {
			q.logger.Errorw("抢占确认单失败", "invocation_id", inv.ID, "error", claimErr)
			cancel()
			continue
		}
		if !claimed {
			cancel()
			continue
		}
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(inv.Arguments), &args)
		// 审批后目标工具可能已从工具面消失（服务器被禁用/删除、工具被停用或隔离）。
		// 这类可预期失败必须 fail-closed 并落稳定错误码，不能掉进内置分支退化成 internal_error。
		if inv.Provider == "mcp" && (q.tools == nil || !q.tools.HasProviderTool(ctx, job.TenantID, inv.ToolName)) {
			q.finalize(ctx, inv.ID, nil, &ToolExecutionError{
				Code:    ErrorCodeToolNotFound,
				Message: "外部工具当前不可用（服务器已禁用/删除，或工具已停用/隔离）",
			}, startedAt, nil)
			cancel()
			continue
		}
		var res interface{}
		// M1-02：外部 provider（MCP）工具——无论读写，审批通过后走专用入口
		// （写工具只有这条路可达；只读工具正常不会进入队列）。
		if q.tools != nil && q.tools.HasProviderTool(ctx, job.TenantID, inv.ToolName) {
			// 单次执行、不重试：失败即终态（避免重复副作用），错误码与耗时由 finalize 落库。
			execution, execErr := q.tools.ExecuteApprovedWrite(ctx, job.TenantID, inv.ToolName, args)
			if execution != nil {
				res = execution.Value
			}
			q.finalize(ctx, inv.ID, res, execErr, startedAt, execution)
			cancel()
			continue
		}
		// 优先委派给 ToolRegistry：写工具的参数解析/租户校验/CMDB 本体绑定
		// 只在 ToolRegistry.Execute 里维护一份，避免此处内联实现与之漂移
		// （历史上这里的 create_ticket 就漏了 category/type/ticket_type_id/ci_id）。
		// 仅当注册表未装配对应领域服务时，才回落到下面的内联实现。
		if q.tools != nil && q.tools.canExecuteWriteTool(inv.ToolName) {
			// M0-11：写路径同样带审计元数据（MCP 写工具在 M1-02 接入后自动获得三元组）。
			execution, execErr := q.tools.ExecuteWithMeta(ctx, job.TenantID, inv.ToolName, q.withInvocationUser(args, inv.UserID))
			err = execErr
			if execution != nil {
				res = execution.Value
			}
			q.finalize(ctx, inv.ID, res, err, startedAt, execution)
			cancel()
			continue
		}
		// execute danger tools via TicketService
		switch inv.ToolName {
		case "create_ticket":
			if q.tickets == nil {
				q.tickets = NewTicketService(&TicketServiceConfig{
					Repository: ticketrepo.NewEntRepository(q.client, zap.NewNop().Sugar()),
					Client:     q.client,
					Logger:     zap.NewNop().Sugar(),
				})
			}
			title, _ := args["title"].(string)
			desc, _ := args["description"].(string)
			priority, _ := args["priority"].(string)
			requesterID := 0
			if v, ok := args["requester_id"].(float64); ok {
				requesterID = int(v)
			}
			r := &dto.CreateTicketRequest{Title: title, Description: desc, Priority: priority, RequesterID: requesterID}
			res, err = q.tickets.CreateTicket(ctx, r, job.TenantID)
			// 回落路径也要补 CMDB 本体绑定，否则审批通过后 ci_id 会被静默丢弃
			if err == nil && q.tools != nil && q.tools.cmdb != nil {
				if v, ok := args["ci_id"].(float64); ok && int(v) > 0 {
					if created, ok := res.(*dto.TicketResponse); ok && created != nil {
						if linkErr := q.tools.cmdb.LinkTicketToCI(ctx, job.TenantID, int(v), created.ID); linkErr != nil {
							q.logger.Warnw("Failed to link ticket to CI after approval",
								"invocation_id", inv.ID, "ticket_id", created.ID, "ci_id", int(v), "error", linkErr)
						}
					}
				}
			}
		case "update_ticket":
			if q.tickets == nil {
				q.tickets = NewTicketService(&TicketServiceConfig{
					Repository: ticketrepo.NewEntRepository(q.client, zap.NewNop().Sugar()),
					Client:     q.client,
					Logger:     zap.NewNop().Sugar(),
				})
			}
			ticketID := 0
			if v, ok := args["ticket_id"].(float64); ok {
				ticketID = int(v)
			}
			status, _ := args["status"].(string)
			assigneeID := 0
			if v, ok := args["assignee_id"].(float64); ok {
				assigneeID = int(v)
			}
			r := &dto.UpdateTicketRequest{Status: status, AssigneeID: assigneeID}
			res, err = q.tickets.UpdateTicket(ctx, ticketID, r, job.TenantID, 0, "") // 0=系统操作，跳过 DataScope
		case "create_ticket_type":
			if q.ticketTypes == nil {
				q.ticketTypes = NewTicketTypeService(q.client, zap.NewNop().Sugar())
			}
			code, _ := args["code"].(string)
			name, _ := args["name"].(string)
			desc, _ := args["description"].(string)
			defaultPriority, _ := args["default_priority"].(string)
			if defaultPriority == "" {
				defaultPriority = "medium"
			}
			icon, _ := args["icon"].(string)
			color, _ := args["color"].(string)
			r := &dto.CreateTicketTypeRequest{
				Code:            code,
				Name:            name,
				Description:     desc,
				DefaultPriority: defaultPriority,
				Icon:            icon,
				Color:           color,
			}
			res, err = q.ticketTypes.CreateTicketType(ctx, r, job.TenantID, inv.UserID)
		default:
			res, err = q.tools.Execute(ctx, job.TenantID, inv.ToolName, args)
		}
		q.finalize(ctx, inv.ID, res, err, startedAt, nil)
		cancel()
	}
}

// finalize 把工具执行结果写回 ToolInvocation（成功/失败两态）。
// M0-11：补耗时与稳定错误码；成功时补 output_summary（脱敏截断，不落原始大结果）。
// M1-02：当执行元数据携带来源三元组时一并回填（pending 创建时已写入，此处兜底纠偏）。
func (q *ToolQueue) finalize(ctx context.Context, invocationID int, res interface{}, err error, startedAt time.Time, execution *ToolExecution) {
	durationMs := time.Since(startedAt).Milliseconds()
	if durationMs == 0 {
		durationMs = 1 // 与 provider 口径一致：成功/失败耗时不出现 0
	}
	applySource := func(update *ent.ToolInvocationUpdateOne) *ent.ToolInvocationUpdateOne {
		if execution == nil {
			return update
		}
		if execution.Provider != "" {
			update = update.SetProvider(execution.Provider)
		}
		if execution.ServerName != "" {
			update = update.SetMcpServerName(execution.ServerName)
		}
		if execution.RawToolName != "" {
			update = update.SetMcpRawToolName(execution.RawToolName)
		}
		if execution.CallableName != "" {
			update = update.SetMcpCallableName(execution.CallableName)
		}
		return update
	}
	if err != nil {
		if _, updateErr := applySource(q.client.ToolInvocation.UpdateOneID(invocationID).
			SetStatus("failed").
			SetError(redact.Summary(err.Error(), 512)).
			SetDurationMs(int(durationMs)).
			SetErrorCode(errorCodeOf(err)).
			// B1-06：最近一次消费错误码（与 error_code 同值，供队列恢复/告警按列筛选）。
			SetLastErrorCode(errorCodeOf(err))).
			Save(ctx); updateErr != nil {
			q.logger.Errorw("Failed to update tool invocation status to failed", "invocation_id", invocationID, "error", updateErr)
		}
		return
	}
	out, _ := json.Marshal(res)
	verifyState, verifyNote := VerifyStateSkipped, ""
	if q.verifier != nil {
		// B1-06：回读校验。失败只影响 verify_state（业务执行已成功），不改变终态语义。
		verifyState, verifyNote = q.verifyResult(invocationID, res)
	}
	if _, updateErr := applySource(q.client.ToolInvocation.UpdateOneID(invocationID).
		SetStatus("done").
		SetResult(string(out)).
		SetDurationMs(int(durationMs)).
		SetOutputSummary(redact.ValueSummary(res, 512)).
		SetVerifyState(verifyState).
		SetVerifyNote(verifyNote)).
		Save(ctx); updateErr != nil {
		q.logger.Errorw("Failed to update tool invocation status to done", "invocation_id", invocationID, "error", updateErr)
	}
}

// withInvocationUser 把发起审批的用户ID回填进参数，让 ToolRegistry 能正确归属
// verifyResult 读取确认单的落库参数并执行回读校验（B1-06）。
//
// 说明：参数以 `tool_invocations.arguments` **落库快照**为准（审批后模型不可改参），
// 校验器只看执行真源，避免与请求侧内存参数漂移。
func (q *ToolQueue) verifyResult(invocationID int, result interface{}) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inv, err := q.client.ToolInvocation.Get(ctx, invocationID)
	if err != nil {
		return VerifyStateSkipped, "读取确认单失败，跳过回读"
	}
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(inv.Arguments), &args)
	state, note := q.verifier.Verify(ctx, inv.TenantID, inv.ToolName, args, result)
	if state == "" {
		state = VerifyStateSkipped
	}
	return state, note
}

// withInvocationUser 把发起审批的用户ID回填进参数，让 ToolRegistry 能正确归属
// requester_id / 创建人（LLM 生成的参数里通常不含用户ID）。
func (q *ToolQueue) withInvocationUser(args map[string]interface{}, userID int) map[string]interface{} {
	if userID <= 0 {
		return args
	}
	merged := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		merged[k] = v
	}
	if _, ok := merged["user_id"]; !ok {
		merged["user_id"] = float64(userID)
	}
	return merged
}

// errorCoder 由外部 provider 的稳定错误实现（避免 service → mcp/provider 的反向依赖）。
type errorCoder interface{ ErrorCode() string }

// errorCodeOf 提取稳定错误码；未知错误返回 "internal_error"（与前端展示口径对齐）。
func errorCodeOf(err error) string {
	if err == nil {
		return ""
	}
	// B1-05：工单乐观锁冲突（update_ticket 的 expected_version 不匹配）——
	// 稳定错误码，供审计与前端提示「刷新后重试」。
	var versionConflict *common.VersionConflictError
	if errors.As(err, &versionConflict) {
		return "tool_version_conflict"
	}
	var coded errorCoder
	if errors.As(err, &coded) {
		if code := coded.ErrorCode(); code != "" {
			return code
		}
	}
	return "internal_error"
}
