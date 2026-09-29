package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"itsm-backend/dto"
	"itsm-backend/ent"
	"itsm-backend/metrics"
	"itsm-backend/middleware"
	"itsm-backend/service"
	"itsm-backend/service/bot"

	"go.uber.org/zap"
)

type Service struct {
	repo               Repository
	logger             *zap.SugaredLogger
	rag                *service.RAGService
	llmGateway         *service.LLMGateway
	tools              *service.ToolRegistry
	queue              *service.ToolQueue
	analytics          *service.AnalyticsService
	prediction         *service.PredictionService
	slaForecastSkill   *service.SLAForecastSkill
	triageService      *service.TriageService
	rca                *service.RootCauseService
	aiTelemetryService *service.AITelemetryService
	// P1-2 修复：SummarizeService 装配路径（替换之前绕行 rca.SummarizeTicket 的方式）
	summarizeService *service.SummarizeService
	// P2-6: ent client，用于复用 RBAC hasResourcePermission
	entClient *ent.Client
	// B0-06：统一脱敏入口（按工具元数据的 default/strict 档位口径）。
	redactor *bot.Redactor
	// B1-01/B1-02：运行态管理器（chatStream 起运行/记步骤/记事件/预算护栏；未注入时零行为变化）。
	botRunner *bot.Manager
	// B1-05：确认单有效期（默认 24h；<=0 表示不设过期，仅测试/离线场景使用）。
	confirmationTTL time.Duration
}

func NewService(
	repo Repository,
	logger *zap.SugaredLogger,
	rag *service.RAGService,
	tools *service.ToolRegistry,
	queue *service.ToolQueue,
	analytics *service.AnalyticsService,
	prediction *service.PredictionService,
	slaForecastSkill *service.SLAForecastSkill,
	triageService *service.TriageService,
	rca *service.RootCauseService,
	aiTelemetryService *service.AITelemetryService,
) *Service {
	return &Service{
		repo:               repo,
		logger:             logger,
		rag:                rag,
		tools:              tools,
		queue:              queue,
		analytics:          analytics,
		prediction:         prediction,
		slaForecastSkill:   slaForecastSkill,
		triageService:      triageService,
		rca:                rca,
		aiTelemetryService: aiTelemetryService,
		redactor:           bot.NewRedactor(),
	}
}

// SetLLMGateway wires an optional LLM gateway for streaming answers.
// Kept as a setter to avoid churning existing NewService call sites.
func (s *Service) SetLLMGateway(gateway *service.LLMGateway) {
	s.llmGateway = gateway
}

// SetSummarizeService wires SummarizeService（2026-09-06 P1-2 修复：替换之前绕行 rca 的方式）。
// SummarizeTicket 优先走 SummarizeService.SummarizeWithMetadata，失败兜底回 rca。
func (s *Service) SetSummarizeService(svc *service.SummarizeService) {
	s.summarizeService = svc
}

// SetEntClient wires the ent client for RBAC permission checks.
// P2-6: AI 工具执行前调用 hasResourcePermission 校验用户对工具 Resource/Action 的权限
func (s *Service) SetEntClient(client *ent.Client) {
	s.entClient = client
}

// SetBotRunner 注入运行态管理器（B1-02；预算参数由装配方从 BP8 配置映射）。
//
// 未注入（nil）时聊天路径不产生任何 bot_runs/bot_steps/bot_events 写入，
// 行为与既有版本零差异（关闭态口径，与 `bot.enabled=false` 对应）。
func (s *Service) SetBotRunner(manager *bot.Manager) {
	s.botRunner = manager
}

// SetBotRunStore 注入运行态存储（B1-01 入口，等价于使用默认预算的 Manager）。
//
// 保留该入口是为了让「只想落运行档案」的调用方（含既有测试）不必了解预算参数；
// 需要按 BP8 配置调预算时改用 SetBotRunner。
func (s *Service) SetBotRunStore(store *bot.RunStore) {
	s.botRunner = bot.NewManager(store, bot.DefaultBudget())
}

// Tool Methods

func (s *Service) ListTools() []service.ToolDefinition {
	if s.tools == nil {
		return nil
	}
	return s.tools.ListTools()
}

// ListToolsForTenant 返回按租户动态化的工具清单（list_cis 的 ci_type 枚举来自租户 CIType 表）。
func (s *Service) ListToolsForTenant(ctx context.Context, tenantID int) []service.ToolDefinition {
	if s.tools == nil {
		return nil
	}
	return s.tools.ListToolsForTenant(ctx, tenantID)
}

// ErrToolPermissionDenied 工具权限不足（P2-6 Gate 2）
var ErrToolPermissionDenied = fmt.Errorf("tool permission denied")

// ErrUnknownTool 未知工具（P2-6 Gate 2）
var ErrUnknownTool = fmt.Errorf("unknown tool")

var ErrToolUnavailable = fmt.Errorf("tool authorization dependencies unavailable")

// M1-02 审批链路错误（fail-closed：调用方可据此决定是否重试，绝不静默丢弃）。
var (
	// ErrInvocationNotPending：审批状态机保护——只有 pending 记录可被审批（防重复执行写工具）。
	ErrInvocationNotPending = fmt.Errorf("tool invocation is not pending approval")
	// ErrToolQueueUnavailable：执行队列不可用/已满；审批不落 approved，保持 pending 可重试。
	ErrToolQueueUnavailable = fmt.Errorf("tool queue unavailable")
	// ErrInvocationExpired：确认单已过期（B1-05）——不可执行，需重新发起确认。
	ErrInvocationExpired = fmt.Errorf("tool invocation confirmation expired")
	// ErrInvocationStateConflict：与既有决策冲突（B1-05：异人决策/改判/生命周期终止）。
	//
	// 包装 ErrInvocationNotPending：终态不可再决策这一既有不变量保持不变（老调用方
	// 仍可用 errors.Is(err, ErrInvocationNotPending) 判定），新调用方按冲突错误细分提示。
	ErrInvocationStateConflict = fmt.Errorf("%w: decision conflicts with existing state", ErrInvocationNotPending)
)

// ExecuteTool 执行 AI 工具
// P2-6: 新增 userID 和 role 参数用于 Gate 2 RBAC 校验
//
// 校验分层：
//
//	Gate 1: 路由级 RBACMiddleware（/api/v1/agent/* 检查 ai:read/ai:write）— 调用前已完成
//	Gate 2: 工具级 RBAC（本方法）— 按 ToolDefinition.Resource/Action 校验
//	Gate 3: 审批流（写工具 !ReadOnly）— 由 NeedsApproval 处理
func (s *Service) ExecuteTool(ctx context.Context, userID, tenantID int, role, name string, args map[string]interface{}) (interface{}, int, error) {
	return s.ExecuteToolWithConversation(ctx, userID, tenantID, role, name, args, 0)
}

// conversationIDKey 在调用链内部传递会话归属（B0-03）。
//
// 仅用于把聊天路径的 conversation_id 带到审计写入点；不承载任何安全语义
// （租户/用户仍走显式参数），无值时行为与既有调用完全一致。
type conversationIDKey struct{}

func withConversationID(ctx context.Context, id int) context.Context {
	if id <= 0 {
		return ctx
	}
	return context.WithValue(ctx, conversationIDKey{}, id)
}

func conversationIDFrom(ctx context.Context) int {
	if v, ok := ctx.Value(conversationIDKey{}).(int); ok && v > 0 {
		return v
	}
	return 0
}

// ExecuteToolWithConversation 与 ExecuteTool 相同，并注入会话归属（B0-03）：
// 聊天路径调用时携带 conversationId，工具调用审计可按会话回溯；
// run_id 待 B1-01 落表后由同一入口注入（字段已由 B0-02 预置）。
func (s *Service) ExecuteToolWithConversation(ctx context.Context, userID, tenantID int, role, name string, args map[string]interface{}, conversationID int) (interface{}, int, error) {
	return s.ExecuteToolWithOptions(ctx, userID, tenantID, role, name, args, ExecuteToolOptions{ConversationID: conversationID})
}

// ExecuteToolOptions 是工具调用的可选上下文（零值 = 既有行为）。
type ExecuteToolOptions struct {
	// ConversationID 会话归属（B0-03）；0 = 不落 conversation_id。
	ConversationID int
	// DryRun 请求写工具预览（B0-04）：零业务写入，仅生成预览快照并留痕。
	DryRun bool
}

// ExecuteToolWithOptions 是执行入口的完整形态（B0-03/B0-04）；其余入口均为其薄封装。
func (s *Service) ExecuteToolWithOptions(ctx context.Context, userID, tenantID int, role, name string, args map[string]interface{}, opts ExecuteToolOptions) (interface{}, int, error) {
	ctx = withConversationID(ctx, opts.ConversationID)
	if userID <= 0 || tenantID <= 0 {
		return nil, 0, ErrToolPermissionDenied
	}
	if s.tools == nil || s.entClient == nil {
		return nil, 0, ErrToolUnavailable
	}

	// === P2-6 Gate 2: 工具级 RBAC 校验 ===
	permCheck := "passed"
	permReason := ""
	allowed := true

	// M0-09：解析含外部 provider（MCP）——同一投影/解析函数，内置优先。
	toolDef := s.tools.GetToolForTenant(ctx, tenantID, name)
	if toolDef == nil {
		// 未知工具：记录 denied 审计，返回错误
		s.recordToolAudit(ctx, tenantID, userID, role, name, args, "denied", "unknown tool", "", nil, false, nil)
		return nil, 0, ErrUnknownTool
	}

	{
		if role != "" && middleware.HasResourcePermission(ctx, s.entClient, role, toolDef.Resource, toolDef.Action, tenantID) {
			permCheck = "passed"
		} else {
			permCheck = "denied"
			permReason = fmt.Sprintf("role=%s lacks %s:%s", role, toolDef.Resource, toolDef.Action)
			allowed = false
			s.logger.Warnw("AI tool RBAC denied",
				"user_id", userID, "tenant_id", tenantID, "role", role,
				"tool", name, "resource", toolDef.Resource, "action", toolDef.Action,
				"enforce", true)
		}
	}

	// Authorization is mandatory; feature flags must never bypass it.
	if !allowed {
		s.recordToolAudit(ctx, tenantID, userID, role, name, args, permCheck, permReason, "", nil, false, nil)
		return nil, 0, fmt.Errorf("%w: %s", ErrToolPermissionDenied, permReason)
	}

	// Check if needs approval
	needsApproval := !toolDef.ReadOnly

	// B0-04：dry-run 分支——写工具预览，零业务写入，不进审批队列。
	if opts.DryRun {
		return s.executeDryRun(ctx, tenantID, userID, role, name, args, permCheck, permReason, toolDef)
	}

	if !needsApproval {
		// M0-11：带审计元数据执行（provider/三元组/耗时/错误码/输出摘要）。
		execution, err := s.tools.ExecuteWithMeta(ctx, tenantID, name, args)
		var res interface{}
		if execution != nil {
			res = execution.Value
		}
		// 只读工具执行也记录审计（AGENTS.md: AI tool invocation must produce audit logs）
		// P2-6: 同步写入 RBAC 校验结果字段
		if err == nil {
			s.recordToolAudit(ctx, tenantID, userID, role, name, args, permCheck, permReason, "executed", nil, false, execution)
		} else {
			// 失败同样留痕（稳定错误码 + 三元组），便于事后定位外部工具故障。
			s.recordToolAudit(ctx, tenantID, userID, role, name, args, permCheck, permReason, "failed", nil, false, execution)
		}
		return res, 0, err
	}

	// 写工具：创建 pending invocation，等待审批
	argsStr, _ := json.Marshal(args)
	// M1-02：来源三元组与风险随 pending **一次落库**——审批人在信息完整（服务器/原始名/投影名/风险）下决策；
	// 风险展示走审批详情实时解析（见 handler），此处落三元组保证即使服务器后续不可达也可追溯来源。
	sourceProvider := toolDef.Provider
	if sourceProvider == "" {
		sourceProvider = service.ProviderNameBuiltin
	}
	callableName := ""
	if sourceProvider != service.ProviderNameBuiltin {
		callableName = toolDef.Name
	}
	// B0-05：写工具统一幂等键（只存 hash；作用域 = 租户 + 发起人 + 工具 + 目标 + 参数）。
	// 读工具不生成键（Idempotent=false，B0-01 已按 ReadOnly 推导）。
	idemHash := ""
	if toolDef.Idempotent {
		targetType, targetID := bot.TargetFromArgs(args)
		hash, keyErr := bot.BuildKey(bot.KeyInput{
			TenantID:   tenantID,
			UserID:     userID,
			Tool:       name,
			TargetType: targetType,
			TargetID:   targetID,
			Args:       args,
		})
		if keyErr != nil {
			// fail-closed：无法生成幂等键时不降级为「普通提交」（否则重复提交会重复落地）。
			return nil, 0, keyErr
		}
		idemHash = hash
		// 顺序重复提交 → 直接回放首次结果（不新增记录、不重复进审批队列）。
		if existing, lookupErr := s.repo.GetToolInvocationByIdempotencyKey(ctx, tenantID, idemHash); lookupErr == nil && existing != nil {
			return replayInvocation(existing), existing.ID, nil
		}
	}
	inv, err := s.repo.CreateToolInvocation(ctx, &ToolInvocation{
		TenantID:         tenantID,
		ConversationID:   conversationIDFrom(ctx),   // B0-03：聊天路径会话归属（无会话则为 0，不入列）
		RunID:            bot.RunIDFromContext(ctx), // B1-01：运行归属（无运行上下文时为 0，不入列）
		ToolName:         name,
		Arguments:        string(argsStr),
		ArgsRedacted:     s.redactArgs(ctx, tenantID, name, args),
		Status:           "pending",
		NeedsApproval:    true,
		ApprovalState:    "pending",
		UserID:           userID,
		PermissionCheck:  permCheck,
		PermissionReason: permReason,
		RoleSnapshot:     role,
		Provider:         sourceProvider,
		McpServerName:    toolDef.ServerName,
		McpRawToolName:   toolDef.RawToolName,
		McpCallableName:  callableName,
		// B0-05：幂等键 hash（读工具为空串 → 不入列）。
		IdempotencyKeyHash: idemHash,
		// B0-01：元数据快照（调用时冻结，防后续治理变更导致审计歧义）。
		Risk:     toolDef.Risk,
		Category: toolDef.Category,
		// B1-05：确认单有效期（默认 24h；TTL<=0 = 不设期限，仅离线/测试）。
		ExpiresAt: s.newConfirmationExpiry(time.Now()),
	})
	if err != nil {
		// 并发重复提交：唯一索引冲突 → 映射为幂等命中（回查既有记录），而非 500。
		if idemHash != "" && bot.IsUniqueViolation(err) {
			if existing, lookupErr := s.repo.GetToolInvocationByIdempotencyKey(ctx, tenantID, idemHash); lookupErr == nil && existing != nil {
				return replayInvocation(existing), existing.ID, nil
			}
			return nil, 0, bot.ErrDuplicateKey
		}
		return nil, 0, err
	}

	return nil, inv.ID, nil
}

// SetConfirmationTTL 设置确认单有效期（B1-05；bootstrap 从配置注入）。
// d <= 0 表示不设过期（离线/测试），生产默认走 bot.DefaultConfirmationTTL。
func (s *Service) SetConfirmationTTL(d time.Duration) { s.confirmationTTL = d }

// newConfirmationExpiry 依据 TTL 计算过期时间；TTL<=0 返回 nil（不设期限）。
func (s *Service) newConfirmationExpiry(now time.Time) *time.Time {
	if s.confirmationTTL <= 0 {
		return nil
	}
	expires := now.Add(s.confirmationTTL)
	return &expires
}

// redactionProfileFor 按工具元数据解析脱敏档（B0-06）。
// 走 GetToolForTenant（内置优先 → provider 兜底），使 MCP 工具的治理标注同样生效；
// 工具未知/未装配 → strict（最保守，与 B0-01 兜底口径一致）。
func (s *Service) redactionProfileFor(ctx context.Context, tenantID int, toolName string) bot.Profile {
	if s.tools == nil {
		return bot.ProfileStrict
	}
	td := s.tools.GetToolForTenant(ctx, tenantID, toolName)
	if td == nil {
		return bot.ProfileStrict
	}
	return bot.NormalizeProfile(td.RedactionProfile)
}

// redactArgs 生成入参脱敏快照（审计/展示唯一来源；B0-06）。
func (s *Service) redactArgs(ctx context.Context, tenantID int, toolName string, args map[string]interface{}) string {
	if s.redactor == nil {
		s.redactor = bot.NewRedactor()
	}
	return s.redactor.RedactArgs(s.redactionProfileFor(ctx, tenantID, toolName), args, 0)
}

// replayInvocation 构造幂等回放载荷（B0-05）：不重复执行，返回首次记录的状态与结果。
func replayInvocation(inv *ToolInvocation) map[string]interface{} {
	payload := map[string]interface{}{
		"idempotentReplay": true,
		"invocationId":     inv.ID,
		"status":           inv.Status,
		"approvalState":    inv.ApprovalState,
		"toolName":         inv.ToolName,
	}
	if inv.Result != nil && *inv.Result != "" {
		var decoded interface{}
		if json.Unmarshal([]byte(*inv.Result), &decoded) == nil {
			payload["result"] = decoded
		} else {
			payload["result"] = *inv.Result
		}
	}
	return payload
}

// recordToolAudit 统一记录只读工具执行审计，包含 P2-6 RBAC 校验结果
//
// L8 修复（2026-09-08）：审计写入失败不再用 _, _ = 吞掉，改用结构化错误日志 +
// itsm_ai_persist_errors_total{operation="create_tool_invocation"} 计数器埋点，
// 保证 DB 抖动时审计丢失可观测、可告警。
func (s *Service) recordToolAudit(ctx context.Context, tenantID, userID int, role, toolName string, args map[string]interface{}, permCheck, permReason, status string, result *string, needsApproval bool, execution *service.ToolExecution) {
	argsStr, _ := json.Marshal(args)
	audit := &ToolInvocation{
		TenantID:         tenantID,
		ConversationID:   conversationIDFrom(ctx),   // B0-03：会话归属（审计可按 conversation_id 回溯）
		RunID:            bot.RunIDFromContext(ctx), // B1-01：运行归属（无运行上下文时为 0，不入列）
		ToolName:         toolName,
		Arguments:        string(argsStr),
		Status:           status,
		NeedsApproval:    needsApproval,
		ApprovalState:    "auto",
		UserID:           userID,
		PermissionCheck:  permCheck,
		PermissionReason: permReason,
		RoleSnapshot:     role,
		// M0-11/B0-06：脱敏入参快照——审计/展示唯一来源（Arguments 仍是执行真源，审批重放依赖它）。
		ArgsRedacted: s.redactArgs(ctx, tenantID, toolName, args),
	}
	if execution != nil {
		audit.Provider = execution.Provider
		audit.McpServerName = execution.ServerName
		audit.McpRawToolName = execution.RawToolName
		audit.McpCallableName = execution.CallableName
		audit.DurationMs = execution.DurationMs
		audit.ErrorCode = execution.ErrorCode
		// B0-06：strict 档工具的结果摘要全掩码（只留键名），default 档沿用原语摘要。
		if s.redactionProfileFor(ctx, tenantID, toolName) == bot.ProfileStrict {
			audit.OutputSummary = s.redactor.RedactResult(bot.ProfileStrict, execution.Value, 0)
		} else {
			audit.OutputSummary = execution.OutputSummary
		}
		// B0-01：元数据快照（内置来自注册表、MCP 来自治理标注，均由 ToolExecution 携带）。
		audit.Risk = execution.Risk
		audit.Category = execution.Category
	}
	if _, err := s.repo.CreateToolInvocation(ctx, audit); err != nil {
		s.logger.Errorw("AI tool audit persistence failed",
			"operation", "create_tool_invocation",
			"tenant_id", tenantID,
			"user_id", userID,
			"tool_name", toolName,
			"approval_state", "auto",
			"error", err,
		)
		metrics.AIPersistErrors.WithLabelValues(
			"create_tool_invocation",
			"",
			strconv.Itoa(tenantID),
		).Inc()
	}
}

// executeDryRun 处理写工具的 dry-run 请求（B0-04）：
//   - 调用注册表预览分支（**零业务写入**：仅参数投影 + 只读读取）；
//   - 预览快照（含内容哈希 version）写入本条调用记录的 `result`，供 B1-05 冻结执行参数；
//   - 记录 `dry_run=true / status=preview`，不进审批队列、不触发任何写路径；
//   - 预览失败同样留痕（稳定错误码），与执行路径同口径。
func (s *Service) executeDryRun(ctx context.Context, tenantID, userID int, role, name string, args map[string]interface{}, permCheck, permReason string, toolDef *service.ToolDefinition) (interface{}, int, error) {
	preview, err := s.tools.PreviewTool(ctx, tenantID, name, args)
	if err != nil {
		code := "preview_failed"
		if errors.Is(err, service.ErrPreviewNotWrite) {
			code = "preview_not_write"
		} else if errors.Is(err, service.ErrPreviewUnsupported) {
			code = "preview_unsupported"
		}
		s.recordToolAudit(ctx, tenantID, userID, role, name, args, permCheck, permReason, "preview_failed", nil, false,
			&service.ToolExecution{
				Provider:  service.ProviderNameBuiltin,
				ErrorCode: code,
				Risk:      toolDef.Risk,
				Category:  toolDef.Category,
			})
		return nil, 0, err
	}

	payload, _ := json.Marshal(preview)
	snapshot := string(payload)
	argsStr, _ := json.Marshal(args)
	inv, err := s.repo.CreateToolInvocation(ctx, &ToolInvocation{
		TenantID:         tenantID,
		ConversationID:   conversationIDFrom(ctx),
		ToolName:         name,
		Arguments:        string(argsStr),
		ArgsRedacted:     s.redactArgs(ctx, tenantID, name, args),
		Result:           &snapshot,
		Status:           "preview",
		NeedsApproval:    false,
		ApprovalState:    "auto",
		UserID:           userID,
		PermissionCheck:  permCheck,
		PermissionReason: permReason,
		RoleSnapshot:     role,
		Provider:         service.ProviderNameBuiltin,
		Risk:             toolDef.Risk,
		Category:         toolDef.Category,
		DryRun:           true,
	})
	if err != nil {
		return nil, 0, err
	}
	return preview, inv.ID, nil
}

// backfillToolDecision 把审批结论结构化回填到会话（B0-03）。
//
// 语义：拒绝写工具后，模型需要知道「该调用被拒 + 原因」才能重新规划；
// 现状该信息只存在于 tool_invocations（会话侧不可见）。回填消息：
//   - 仅当调用记录带 conversation_id 时写入；
//   - role=assistant + 结构化 JSON（type=tool_approval_decision），避免被当作普通用户输入；
//   - reason 经 pkg/redact 统一截断/脱敏，防止审批备注意外带出敏感内容。
func (s *Service) backfillToolDecision(ctx context.Context, inv *ToolInvocation, decision, reason string) {
	if inv == nil || inv.ConversationID <= 0 || s.repo == nil {
		return
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"type":         "tool_approval_decision",
		"invocationId": inv.ID,
		"tool":         inv.ToolName,
		"decision":     decision,
		"reason":       s.redactor.RedactText(s.redactionProfileFor(ctx, inv.TenantID, inv.ToolName), reason, 512),
	})
	if _, err := s.repo.CreateMessage(ctx, &Message{
		ConversationID: inv.ConversationID,
		Role:           "assistant",
		Content:        string(payload),
	}); err != nil {
		s.logger.Errorw("工具审批结论回填会话失败",
			"invocation_id", inv.ID,
			"conversation_id", inv.ConversationID,
			"error", err,
		)
		metrics.AIPersistErrors.WithLabelValues("backfill_tool_decision", "", strconv.Itoa(inv.TenantID)).Inc()
	}
}

// ApproveTool 审批写工具执行请求（M1-02 加固）。
//
// 语义与 fail-closed 保证：
//   - 状态机：仅 `approval_state=pending` 的记录可被审批；重复审批/已执行记录一律拒绝
//     （防止同一写工具被执行两次）；
//   - 拒绝：approval_state=rejected + status=rejected + reason（决策人/时间落 approved_by/approved_at），
//     进入队列的路径不触发；
//   - 通过：先确认队列可用（未装配或已满则**不改状态**直接失败，调用方可稍后重试），
//     再落 approved 并入队；入队失败时回滚为 pending，绝不留下「已批准但永不执行」的悬空记录；
//   - 执行以**落库参数**（`tool_invocations.arguments`）为唯一真源，审批接口不接受任何参数覆盖
//     （参数冻结：审批后篡改无效）。
func (s *Service) ApproveTool(ctx context.Context, id int, tenantID, userID int, approve bool, reason string) (string, error) {
	inv, err := s.repo.GetToolInvocation(ctx, id, tenantID)
	if err != nil {
		return "", err
	}
	now := time.Now()
	state := bot.NormalizeConfirmationState(inv.ApprovalState, inv.Status)

	// B1-05 惰性过期：扫描任务之外的兜底判定——过期单不得执行，也不得被"补批准"。
	if state == bot.ConfirmationPending && bot.IsExpiredAt(inv.ExpiresAt, now) {
		inv.ApprovalState = string(bot.ConfirmationExpired)
		inv.Status = string(bot.ConfirmationExpired)
		if _, updateErr := s.repo.UpdateToolInvocation(ctx, inv); updateErr != nil {
			// 过期标记失败不阻断拒绝语义：向调用方报过期，避免误批准。
			s.logger.Warnw("确认单过期标记失败", "invocation_id", inv.ID, "tenant_id", tenantID, "error", updateErr)
		} else {
			s.backfillToolDecision(ctx, inv, "expired", "确认单已过期，请重新发起")
		}
		return "", ErrInvocationExpired
	}

	priorApprove := inv.ApprovalState == "approved" || inv.ApprovalState == string(bot.ConfirmationConfirmed)
	switch bot.EvaluateDecision(state, inv.ApprovedBy, userID, approve, priorApprove) {
	case bot.DecisionReplay:
		// B1-05 幂等回放：同人同向重试（网络抖动/前端重发）返回既有决策，不重复执行、不改库。
		if priorApprove {
			return "approved", nil
		}
		return "rejected", nil
	case bot.DecisionConflict:
		return "", ErrInvocationStateConflict
	case bot.DecisionApply:
		// 落决策（下方按 approve 分支处理）。
	default:
		return "", ErrInvocationStateConflict
	}

	if !approve {
		inv.ApprovalState = "rejected"
		inv.Status = "rejected"
		inv.ApprovalReason = reason
		inv.ApprovedBy = userID
		inv.ApprovedAt = &now
		_, err = s.repo.UpdateToolInvocation(ctx, inv)
		if err == nil {
			// B0-03：拒绝原因结构化回填会话，供模型/后续轮次重新规划（无会话归属则跳过）。
			s.backfillToolDecision(ctx, inv, "rejected", reason)
		}
		return "rejected", err
	}

	// 队列可用性前置检查：不在「无法执行」时把记录置为 approved。
	if s.queue == nil {
		return "", ErrToolQueueUnavailable
	}

	inv.ApprovalState = "approved"
	inv.ApprovedBy = userID
	inv.ApprovedAt = &now
	_, err = s.repo.UpdateToolInvocation(ctx, inv)
	if err != nil {
		return "", err
	}

	if err := s.queue.Enqueue(service.ToolJob{
		InvocationID: inv.ID,
		TenantID:     tenantID,
	}); err != nil {
		// 回滚为 pending：审批人可稍后重试，避免出现「已批准但无执行、无结果」的悬空记录。
		inv.ApprovalState = "pending"
		inv.ApprovedBy = 0
		inv.ApprovedAt = nil
		if _, rollbackErr := s.repo.UpdateToolInvocation(ctx, inv); rollbackErr != nil {
			s.logger.Errorw("审批入队失败且回滚 pending 失败（需人工介入）",
				"invocation_id", inv.ID, "tenant_id", tenantID, "error", rollbackErr)
		}
		s.logger.Warnw("审批入队失败（队列满），已回滚为 pending",
			"invocation_id", inv.ID, "tenant_id", tenantID, "error", err)
		return "", fmt.Errorf("%w: %v", ErrToolQueueUnavailable, err)
	}

	return "approved", nil
}

// Chat and RAG

func (s *Service) Chat(ctx context.Context, tenantID, userID int, query string, limit int, convID int) (interface{}, int, error) {
	s.logger.Infow("AI Chat", "query", query, "tenantID", tenantID)

	items, err := s.rag.Ask(ctx, tenantID, query, limit)
	if err != nil {
		return nil, 0, err
	}

	// Persist conversation
	if convID == 0 {
		conv, err := s.repo.CreateConversation(ctx, &Conversation{
			Title:    "AI 对话",
			UserID:   userID,
			TenantID: tenantID,
		})
		if err == nil {
			convID = conv.ID
		}
	}

	if convID != 0 {
		// L8 修复：Chat 路径持久化失败必须可见。两类消息分别打点，避免一次失败掩盖另一类错误。
		if _, err := s.repo.CreateMessage(ctx, &Message{
			ConversationID: convID,
			Role:           "user",
			Content:        query,
		}); err != nil {
			s.logger.Errorw("AI chat persist user message failed",
				"operation", "create_message",
				"tenant_id", tenantID,
				"user_id", userID,
				"conversation_id", convID,
				"role", "user",
				"error", err,
			)
			metrics.AIPersistErrors.WithLabelValues(
				"create_message",
				"user",
				strconv.Itoa(tenantID),
			).Inc()
		}
		payload, _ := json.Marshal(items)
		if _, err := s.repo.CreateMessage(ctx, &Message{
			ConversationID: convID,
			Role:           "assistant",
			Content:        string(payload),
		}); err != nil {
			s.logger.Errorw("AI chat persist assistant message failed",
				"operation", "create_message",
				"tenant_id", tenantID,
				"user_id", userID,
				"conversation_id", convID,
				"role", "assistant",
				"error", err,
			)
			metrics.AIPersistErrors.WithLabelValues(
				"create_message",
				"assistant",
				strconv.Itoa(tenantID),
			).Inc()
		}
	}

	return items, convID, nil
}

// ResolveChatProvider 只解析、不调用模型：供 /ai/chat（纯 RAG 检索，无 LLM 调用）
// 与 /ai/chat/stream 回带 provider/providerSource，以及请求级校验（BE-7，§3.3/§3.4）。
//
// 语义刻意分叉：
//   - provider 非空（系统管理员显式覆盖）：任何解析失败都透出（404/409/422/503），
//     绝不静默回退——静默回退会让调用方误以为指定实例已生效；
//   - provider 为空（个人默认 → 租户默认 → 静态 默认链）：解析失败仅告警并返回零标注，
//     调用方按现状继续（开关关闭/未配置实例时保持零破坏语义）。
func (s *Service) ResolveChatProvider(ctx context.Context, tenantID, userID int, provider string) (service.ProviderResolution, error) {
	explicit := strings.TrimSpace(provider)
	if s.llmGateway == nil {
		if explicit == "" {
			return service.ProviderResolution{}, nil
		}
		return service.ProviderResolution{Key: explicit, Source: service.ProviderSourceRequest},
			fmt.Errorf("%w: LLM 网关未初始化，无法解析 provider=%q", service.ErrProviderUnavailable, explicit)
	}
	req := service.ProviderRequest{Key: explicit, TenantID: tenantID, UserID: userID}
	resolution, err := s.llmGateway.ResolveRequest(ctx, req)
	if err != nil {
		if explicit != "" {
			return resolution, err
		}
		s.logger.Warnw("AI chat provider 默认链解析失败，保留现状语义继续",
			"error", err, "tenantID", tenantID, "userID", userID)
		return service.ProviderResolution{}, nil
	}
	return resolution, nil
}

// ChatWithProviderInfo 与 Chat 相同，并回带解析出的 provider 标注（BE-7）：
// 解析失败时按 §3.4 契约返回（由 handler 映射 404/409/422/503），不做降级应答。
func (s *Service) ChatWithProviderInfo(ctx context.Context, tenantID, userID int, query string, limit, convID int, provider string) (interface{}, service.ProviderResolution, int, error) {
	resolution, err := s.ResolveChatProvider(ctx, tenantID, userID, provider)
	if err != nil {
		return nil, resolution, 0, err
	}
	answers, convIDOut, err := s.Chat(ctx, tenantID, userID, query, limit, convID)
	if err != nil {
		return nil, resolution, convIDOut, err
	}
	return answers, resolution, convIDOut, nil
}

// chatGateway 返回本次聊天应使用的网关及其绑定输入（BE-7）：
//   - 开关关闭且无显式覆盖：原样返回既有网关、nil 绑定（旧路径逐字节不变，QA-3）；
//   - 否则：返回绑定了解析输入的浅拷贝，Chat/ChatStream/SupportsToolCalling 按 §3.3 路由。
func (s *Service) chatGateway(tenantID, userID int, provider string) (*service.LLMGateway, *service.ProviderRequest) {
	if s.llmGateway == nil {
		return nil, nil
	}
	if strings.TrimSpace(provider) == "" && !service.MultiProviderEnabled() {
		return s.llmGateway, nil
	}
	req := service.ProviderRequest{Key: strings.TrimSpace(provider), TenantID: tenantID, UserID: userID}
	return s.llmGateway.WithProviderRequest(req), &req
}

// chatWritableTools 是允许注入聊天路径的写工具白名单。
// 这些工具经由 ExecuteTool 的审批流（创建 pending invocation + 入队等待人工审批），
// 绝不在聊天链路内直接落库，因此可安全暴露给 LLM 自主决策调用。
var chatWritableTools = map[string]bool{
	"create_ticket":      true,
	"update_ticket":      true,
	"create_ticket_type": true,
	// CMDB 本体链路：把工单挂到配置项（走同一审批流）
	"link_ticket_ci": true,
	// CMDB 关系写操作：与上面同一审批流 + Gate2 RBAC（resource=ci_relationship, action=write）
	// LLM 在聊天中建/删 CI 关系必须走人工审批，与原生 UI 路径行为一致。
	"create_ci_relationship": true,
	"delete_ci_relationship": true,
}

// ChatStream streams a RAG answer through onDelta while emitting sources
// separately via onSources. It also persists the resulting conversation and
// messages after the stream completes so history is preserved.
//
// P1-B: 注入按权限过滤后的只读工具。只有当前角色拥有 resource:action 权限的只读工具
// 才会进入聊天路径；写工具（!ReadOnly）绝不注入。模型发起的工具调用经由 execTool
// 复用 ExecuteTool 的 RBAC Gate 2 校验 + 审计记录，执行结果回填后继续流式生成。
func (s *Service) ChatStream(
	ctx context.Context,
	tenantID, userID int,
	role string,
	query string,
	limit int,
	convID int,
	onSources func([]map[string]any),
	onDelta func(string),
	onTool func(ToolStreamEvent),
	onRun func(eventType string, payload map[string]any),
) (int, string, error) {
	gateway, providerReq := s.chatGateway(tenantID, userID, "")
	return s.chatStream(ctx, tenantID, userID, role, query, limit, convID, gateway, providerReq, onSources, onDelta, onTool, onRun)
}

// ChatStreamWithProviderInfo 同 ChatStream，但按 §3.3 解析 provider（BE-7）：
// 显式覆盖的解析失败可见地失败（404/409/422/503），默认链失败保留现状语义；
// 返回值额外回带生效 provider 标注，供 SSE done 事件消费。
func (s *Service) ChatStreamWithProviderInfo(
	ctx context.Context,
	tenantID, userID int,
	role string,
	query string,
	limit int,
	convID int,
	provider string,
	onSources func([]map[string]any),
	onDelta func(string),
	onTool func(ToolStreamEvent),
	onRun func(eventType string, payload map[string]any),
) (service.ProviderResolution, int, error) {
	resolution, err := s.ResolveChatProvider(ctx, tenantID, userID, provider)
	if err != nil {
		return resolution, 0, err
	}
	gateway, providerReq := s.chatGateway(tenantID, userID, provider)
	convIDOut, _, err := s.chatStream(ctx, tenantID, userID, role, query, limit, convID, gateway, providerReq, onSources, onDelta, onTool, onRun)
	return resolution, convIDOut, err
}

// chatStream 在既有流式链路外包裹运行态记录（B1-01）：
// 注入 RunStore 时起运行（running）、把 run_id 注入上下文（供 ToolInvocation 贯通），
// 结束后记 llm 步骤 + run_finished 事件并收口运行；未注入时直接透传，
// 行为与既有版本零差异。所有运行态写入失败均只告警，**绝不**影响聊天主链路。
func (s *Service) chatStream(
	ctx context.Context,
	tenantID, userID int,
	role string,
	query string,
	limit int,
	convID int,
	gateway *service.LLMGateway,
	providerReq *service.ProviderRequest,
	onSources func([]map[string]any),
	onDelta func(string),
	onTool func(ToolStreamEvent),
	onRun func(eventType string, payload map[string]any),
) (int, string, error) {
	var run *bot.Run
	startedAt := time.Now()
	budgetAborted := false
	var cancelRun context.CancelFunc
	// admitTool 是工具执行的**预算闸门**（B1-03）：在执行点判定，超限即拒绝执行并收口运行。
	//
	// 口径：计数与拒绝都发生在真正调用工具之前（不是事后记账）；判定失败时收口运行、
	// 置 budgetAborted 并取消本次链路上下文，让模型侧轮次尽快停止。
	// 第二个返回值表示「这是首次拒绝」：模型循环可能在收口后继续尝试调用，重复的拒绝
	// 只影响主链路中止，不再重复写审计/不再重复外发事件（避免一次超限刷出多条记录）。
	budgetRejected := false
	admitTool := func() (error, bool) {
		if run == nil {
			return nil, false
		}
		if err := run.ReserveToolCall(); err != nil {
			first := !budgetRejected
			budgetRejected = true
			budgetAborted = true
			if first {
				if exceedErr := run.BudgetExceeded(context.Background(), "max_tool_calls"); exceedErr != nil {
					s.logger.Warnw("B1-03 预算超限收口失败", "error", exceedErr, "run_id", run.ID())
				}
				if cancelRun != nil {
					cancelRun()
				}
			}
			return fmt.Errorf("本次对话已超出工具调用预算（%s），已停止执行: %w", bot.ErrorCodeBudgetExceeded, bot.ErrBudgetExceeded), first
		}
		return nil, false
	}
	broadcastRun := func(eventType string, payload map[string]any) {
		if onRun == nil || run == nil {
			return
		}
		broadcastRunEvent(onRun, eventType, payload, run)
	}
	if s.botRunner != nil {
		started, err := s.botRunner.Start(ctx, bot.StartRunInput{
			TenantID:       tenantID,
			ConversationID: convID,
			Entrypoint:     "chat",
		})
		if err != nil {
			s.logger.Warnw("B1-01 运行记录创建失败（降级为不记录）", "error", err, "tenant_id", tenantID)
		} else if started != nil {
			run = started
			// B1-03：预算超限需要**中止主链路**，因此运行存在时给本次请求挂可取消 ctx。
			// 取消只影响本次聊天链路，不影响 HTTP 请求上下文（错误帧仍可写出）。
			ctx, cancelRun = context.WithCancel(ctx)
			defer cancelRun()
			ctx = bot.WithRunID(ctx, run.ID())
			startedPayload := map[string]any{
				"entrypoint":     "chat",
				"conversationId": convID,
				"limit":          limit,
			}
			if _, evErr := run.Emit(ctx, "run_started", startedPayload); evErr != nil {
				s.logger.Warnw("B1-01 run_started 事件写入失败", "error", evErr, "run_id", run.ID())
			} else {
				broadcastRun("run_started", startedPayload)
			}
		}
	}

	// B1-02/B1-03：工具事件旁路记录（步骤 + 运行事件），并对前端保持原样透传。
	toolObserver := onTool
	if run != nil {
		toolObserver = s.botToolObserver(run, onRun, onTool)
	}

	outConvID, answer, err := s.chatStreamInner(ctx, tenantID, userID, role, query, limit, convID, gateway, providerReq, onSources, onDelta, toolObserver, admitTool)

	if run != nil {
		// 收口写入与请求取消解耦：客户端断开也要留下完成态与事件（审计同源）。
		finishCtx := context.WithoutCancel(ctx)
		status, code := "completed", ""
		if err != nil {
			status, code = "failed", botRunErrorCode(err)
		}
		if budgetAborted {
			// 预算超限：错误事件的落库与运行收口已由 Run.BudgetExceeded 完成，此处只对齐
			// 返回错误语义（供 HTTP 层输出 error{errorCode=budget_exceeded}）。
			status, code = "failed", bot.ErrorCodeBudgetExceeded
			err = fmt.Errorf("本次对话已超出工具调用预算（%s），执行已停止: %w", bot.ErrorCodeBudgetExceeded, bot.ErrBudgetExceeded)
		} else {
			durationMs := int(time.Since(startedAt).Milliseconds())
			if _, stepErr := run.RecordStep(finishCtx, "llm", "", durationMs); stepErr != nil {
				s.logger.Warnw("B1-01 llm 步骤写入失败", "error", stepErr, "run_id", run.ID())
			} else {
				broadcastRun("step", map[string]any{
					"stepIndex":  run.Steps() - 1,
					"type":       "llm",
					"durationMs": durationMs,
				})
			}
		}
		if _, evErr := run.Emit(finishCtx, "run_finished", map[string]any{
			"status":    status,
			"errorCode": code,
		}); evErr != nil {
			s.logger.Warnw("B1-01 run_finished 事件写入失败", "error", evErr, "run_id", run.ID())
		}
		if finishErr := run.Finish(finishCtx, status, code); finishErr != nil {
			s.logger.Warnw("B1-01 运行收口失败", "error", finishErr, "run_id", run.ID())
		}
	}

	return outConvID, answer, err
}

// broadcastRunEvent 把**已落库**的运行事件转交 SSE 广播（B1-03），并统一补充 runId。
//
// 口径：只在持久化成功之后调用，保证「先落库后广播」；onRun 为 nil 时静默跳过。
func broadcastRunEvent(onRun func(eventType string, payload map[string]any), eventType string, payload map[string]any, run *bot.Run) {
	if onRun == nil || run == nil {
		return
	}
	enriched := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		enriched[key] = value
	}
	enriched["runId"] = run.ID()
	onRun(eventType, enriched)
}

// botToolObserver 包装工具事件回调（B1-02/B1-03）：
//   - 事件原样透传给前端回调（契约不变）；
//   - `done`/`failed` 落一条 tool 步骤（payload_ref = tool_invocation:<id>，无 id 时记工具名）
//     并广播 v2 `step` 事件（与 bot_steps 同源）。
//
// 边界：**预算判定不在本观察者内**（B1-03 起上移到执行点闸门 `admitTool`）——观察者是
// 「已发生事实」的记录者，若在此处做准入判定会晚于实际执行，无法真正中止主链路。
func (s *Service) botToolObserver(
	run *bot.Run,
	onRun func(eventType string, payload map[string]any),
	next func(ToolStreamEvent),
) func(ToolStreamEvent) {
	return func(event ToolStreamEvent) {
		switch event.Status {
		case "done", "failed":
			ref := event.Tool
			if event.ID > 0 {
				ref = "tool_invocation:" + strconv.Itoa(event.ID)
			}
			stepIndex, stepErr := run.RecordStep(context.Background(), "tool", ref, int(event.DurationMs))
			if stepErr != nil {
				s.logger.Warnw("B1-02 工具步骤写入失败", "error", stepErr, "run_id", run.ID(), "tool", event.Tool)
			} else {
				broadcastRunEvent(onRun, "step", map[string]any{
					"stepIndex":  stepIndex,
					"type":       "tool",
					"payloadRef": ref,
					"durationMs": event.DurationMs,
				}, run)
			}
			if _, err := run.Emit(context.Background(), "tool_call", map[string]any{
				"tool":     event.Tool,
				"provider": event.Provider,
				"phase":    event.Phase,
				"status":   event.Status,
				"id":       event.ID,
			}); err != nil {
				s.logger.Warnw("B1-02 工具事件写入失败", "error", err, "run_id", run.ID(), "tool", event.Tool)
			}
		}
		if next != nil {
			next(event)
		}
	}
}

// botRunErrorCode 把主链路错误归类为运行态错误码（用于 bot_runs.error_code）。
func botRunErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "chat_error"
	}
}

// chatStreamInner 是流式聊天主链路的内部实现。gateway/providerReq 决定本次调用实际
// 路由到的 provider：providerReq 为 nil = 未绑定（开关关闭且无覆盖），能力探测与
// 模型调用都保持既有静态口径；非 nil = 绑定副本，按 §3.3 解析链路由。
func (s *Service) chatStreamInner(
	ctx context.Context,
	tenantID, userID int,
	role string,
	query string,
	limit int,
	convID int,
	gateway *service.LLMGateway,
	providerReq *service.ProviderRequest,
	onSources func([]map[string]any),
	onDelta func(string),
	onTool func(ToolStreamEvent),
	admitTool func() (error, bool),
) (int, string, error) {
	s.logger.Infow("AI ChatStream", "query", query, "tenantID", tenantID, "convID", convID, "role", role)

	if s.rag == nil {
		return 0, "", fmt.Errorf("RAG service not initialized")
	}

	// 按权限过滤只读工具注入聊天路径；写工具仅放行经审批流的白名单（创建工单/更新工单/创建工单类型），
	// 它们经由 ExecuteTool 进入待审批队列，绝不在聊天链路内直接落地写库。
	//
	// Provider 能力闸门：部分 provider（如 MiniMax Anthropic 兼容接口）仅实现了
	// Chat，未实现 ChatStreamWithTools。此时 LLMGateway 会退化为普通 ChatStream，
	// 但 System Prompt 依然告诉模型“必须调用工具”，导致模型在文本中假装调用
	// (“正在调用 list_tickets 工具...”) 但永远拿不到结果。修复：检测 provider
	// 能力，不支持时不注入 tools，让 RAG 层退化为纯知识库问答（合规）。
	var tools []service.LLMTool
	// BE-7：绑定路径（多 Provider 开启或显式覆盖）按解析出的 provider 探测工具能力——
	// 无 ctx 的 SupportsToolCalling 只看静态 provider，会把「租户默认不支持工具」误判为
	// 支持，导致模型在文本里假装调用工具却永远拿不到结果。未绑定时维持既有静态口径。
	providerSupportsTools := false
	if gateway != nil {
		if providerReq != nil {
			providerSupportsTools = gateway.SupportsToolCallingRequest(ctx, *providerReq)
		} else {
			providerSupportsTools = gateway.SupportsToolCalling()
		}
	}
	if s.tools != nil && providerSupportsTools {
		// 按租户动态化工具参数（list_cis 的 ci_type 枚举来自租户 CIType 表）
		for _, td := range s.tools.ListToolsForTenant(ctx, tenantID) {
			if !td.ReadOnly && !chatWritableTools[td.Name] {
				continue
			}
			if IsToolRBACEnabled() && s.entClient != nil && role != "" && role != "super_admin" {
				if !middleware.HasResourcePermission(ctx, s.entClient, role, td.Resource, td.Action, tenantID) {
					s.logger.Debugw("AI ChatStream: tool filtered by RBAC",
						"tool", td.Name, "resource", td.Resource, "action", td.Action, "role", role, "tenantID", tenantID)
					continue
				}
			}
			tools = append(tools, service.LLMTool{
				Name:        td.Name,
				Description: td.Description,
				Parameters:  td.ArgsSchema,
			})
		}
	}

	// 工具执行回调：复用 ExecuteTool 的 RBAC Gate 2 + 审计，与 agent 执行路径一致。
	// 写工具经审批流会返回 (res=nil, invID!=0, err=nil)，这里将其转译为结构化
	// approval_pending 提示，让 LLM 明确告知用户"操作已提交、待人工审批"。
	//
	// M1-03：回调内按「开始 → 成功/失败/待审批」发出工具事件（脱敏 + 截断），
	// 事件只作过程可见；无论事件是否下发，最终答案仍由 delta/done 承载。
	execTool := func(name string, args map[string]any) (any, error) {
		// 注入操作者身份，便于工单归属与审计（审批队列回放时据此归因）
		if _, ok := args["user_id"]; !ok {
			args["user_id"] = float64(userID)
		}
		if _, ok := args["requester_id"]; !ok {
			args["requester_id"] = float64(userID)
		}

		// 事件元数据取自同一解析入口（含 MCP 来源与读写性质）；解析不到（未知工具）
		// 时仍发事件，让「失败」在界面上可见，而不是静默消失。
		event := ToolStreamEvent{Tool: name, Provider: service.ProviderNameBuiltin, Phase: "read"}
		if s.tools != nil {
			if def := s.tools.GetToolForTenant(ctx, tenantID, name); def != nil {
				event.Provider = def.Provider
				if event.Provider == "" {
					event.Provider = service.ProviderNameBuiltin
				}
				event.Server = def.ServerName
				if !def.ReadOnly {
					event.Phase = "write"
				}
			}
		}
		emit := func(ev ToolStreamEvent) {
			if onTool != nil {
				onTool(ev)
			}
		}

		started := time.Now()
		// B1-03：预算闸门必须在**执行点**判定——超限即拒绝执行（不产生副作用），
		// 只发一条 failed 事件（带 budget_exceeded），并把错误抛回模型循环以中止主链路。
		if admitTool != nil {
			if admitErr, firstRejection := admitTool(); admitErr != nil {
				event.Status = ToolEventStatusFailed
				event.ErrorCode = toolEventErrorCode(admitErr)
				if firstRejection {
					// 只让前端与审计看到一次预算拒绝；重复拒绝仅用于尽快结束循环。
					emit(event)
				}
				return nil, admitErr
			}
		}
		event.Status = ToolEventStatusStarted
		emit(event)

		finish := func(status, summary, errCode string, invID int) {
			out := event
			out.Status = status
			out.Summary = summary
			out.ErrorCode = errCode
			out.ID = invID
			if status == ToolEventStatusDone || status == ToolEventStatusFailed {
				// 耗时口径与审计一致：亚毫秒记 1ms，避免前端出现「0ms」的歧义。
				out.DurationMs = time.Since(started).Milliseconds()
				if out.DurationMs < 1 {
					out.DurationMs = 1
				}
			}
			emit(out)
		}

		res, invID, err := s.ExecuteTool(ctx, userID, tenantID, role, name, args)
		if err != nil {
			finish(ToolEventStatusFailed, "", toolEventErrorCode(err), 0)
			return nil, err
		}
		if res == nil && invID != 0 {
			// 写路径：本次流内只到「已提交待审批」，执行发生在审批之后。
			finish(ToolEventStatusPending, "", "", invID)
			return map[string]any{
				"status":       "approval_pending",
				"tool":         name,
				"invocationId": invID,
				"message":      fmt.Sprintf("操作已提交，等待人工审批后执行（invocationId=%d）", invID),
			}, nil
		}
		finish(ToolEventStatusDone, toolEventSummary(res, ToolEventSummaryLimit), "", invID)
		return res, nil
	}

	var (
		captured strings.Builder
		sources  []map[string]any
	)

	wrappedSources := func(items []map[string]any) {
		sources = items
		if onSources != nil {
			onSources(items)
		}
	}
	wrappedDelta := func(delta string) {
		captured.WriteString(delta)
		if onDelta != nil {
			onDelta(delta)
		}
	}

	if err := s.rag.AskWithLLMStreamWithTools(ctx, tenantID, query, gateway, limit, tools, wrappedSources, wrappedDelta, execTool); err != nil {
		return 0, "", err
	}

	// Persist conversation and messages after the stream completes so we don't
	// leave partial messages if the client disconnects mid-stream.
	if convID == 0 {
		conv, err := s.repo.CreateConversation(ctx, &Conversation{
			Title:    "AI 对话",
			UserID:   userID,
			TenantID: tenantID,
		})
		if err == nil && conv != nil {
			convID = conv.ID
		}
	}
	if convID != 0 {
		// L8 修复：ChatStream 路径持久化失败同样必须可见，与 Chat 路径共用同一计数器。
		// 流式响应已经写回客户端；如果 DB 写入失败而日志被吞，
		// 用户会看到回复但刷新后历史丢失，难以排查。
		if _, err := s.repo.CreateMessage(ctx, &Message{
			ConversationID: convID,
			Role:           "user",
			Content:        query,
		}); err != nil {
			s.logger.Errorw("AI chatstream persist user message failed",
				"operation", "create_message",
				"tenant_id", tenantID,
				"user_id", userID,
				"conversation_id", convID,
				"role", "user",
				"error", err,
			)
			metrics.AIPersistErrors.WithLabelValues(
				"create_message",
				"user",
				strconv.Itoa(tenantID),
			).Inc()
		}
		payload := map[string]any{
			"answer":  captured.String(),
			"sources": sources,
		}
		buf, _ := json.Marshal(payload)
		if _, err := s.repo.CreateMessage(ctx, &Message{
			ConversationID: convID,
			Role:           "assistant",
			Content:        string(buf),
		}); err != nil {
			s.logger.Errorw("AI chatstream persist assistant message failed",
				"operation", "create_message",
				"tenant_id", tenantID,
				"user_id", userID,
				"conversation_id", convID,
				"role", "assistant",
				"error", err,
			)
			metrics.AIPersistErrors.WithLabelValues(
				"create_message",
				"assistant",
				strconv.Itoa(tenantID),
			).Inc()
		}
	}

	return convID, captured.String(), nil
}

func (s *Service) ListConversations(ctx context.Context, tenantID, userID int) ([]*Conversation, error) {
	return s.repo.ListConversations(ctx, tenantID, userID)
}

func (s *Service) GetConversation(ctx context.Context, id, tenantID int) (*Conversation, error) {
	return s.repo.GetConversation(ctx, id, tenantID)
}

func (s *Service) GetConversationMessages(ctx context.Context, convID, tenantID int) ([]*Message, error) {
	_, err := s.repo.GetConversation(ctx, convID, tenantID)
	if err != nil {
		return nil, err
	}
	return s.repo.GetMessages(ctx, convID)
}

func (s *Service) DeleteConversation(ctx context.Context, id, tenantID int) error {
	return s.repo.DeleteConversation(ctx, id, tenantID)
}

// Root Cause Analysis

func (s *Service) AnalyzeTicket(ctx context.Context, ticketID int, tenantID int) (interface{}, error) {
	return s.rca.AnalyzeTicket(ctx, ticketID, tenantID)
}

func (s *Service) AnalyzeTicketWithAudit(ctx context.Context, ticketID int, tenantID int, userID int) (interface{}, error) {
	result, err := s.rca.AnalyzeTicket(ctx, ticketID, tenantID)
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf("AnalyzeTicket ticketID=%d", ticketID)
	s.persistAIResult(ctx, "rca", tenantID, userID, prompt, result, "", 0, 0, nil)
	return result, nil
}

func (s *Service) AnalyzeIncident(ctx context.Context, incidentID int, tenantID int) (interface{}, error) {
	if s.rca == nil {
		return nil, service.ErrAIAnalysisUnavailable
	}
	return s.rca.AnalyzeIncident(ctx, incidentID, tenantID)
}

func (s *Service) AnalyzeIncidentWithAudit(ctx context.Context, incidentID int, tenantID int, userID int) (interface{}, error) {
	result, err := s.AnalyzeIncident(ctx, incidentID, tenantID)
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf("AnalyzeIncident incidentID=%d", incidentID)
	s.persistAIResult(ctx, "incident_impact", tenantID, userID, prompt, result, "", 0, 0, nil)
	return result, nil
}

// CreateTicketByAI 通过 AI 解析自然语言描述，智能分析并返回工单创建建议
func (s *Service) CreateTicketByAI(ctx context.Context, description string, tenantID int) (map[string]interface{}, error) {
	s.logger.Infow("AI CreateTicketByAI", "tenantID", tenantID)

	// 使用 Triage 服务分析描述，提取分类和优先级
	if s.triageService != nil {
		result := s.triageService.Suggest(ctx, description, description)
		return map[string]interface{}{
			"suggested_title":    description,
			"suggested_category": result.Category,
			"suggested_priority": result.Priority,
			"confidence":         result.Confidence,
			"reasoning":          result.Explanation,
			"tenant_id":          tenantID,
			"status":             "draft",
			"message":            "AI 已分析描述，请确认后提交工单",
		}, nil
	}

	// fallback：基于关键词简单分析
	return map[string]interface{}{
		"suggested_title":    description,
		"suggested_category": "general",
		"suggested_priority": "medium",
		"confidence":         0.5,
		"reasoning":          "基于关键词分析",
		"tenant_id":          tenantID,
		"status":             "draft",
		"message":            "AI 服务不可用，已返回默认建议",
	}, nil
}

// SummarizeTicket B9: AI 工单总结。
//
// 2026-09-06 P1-2 修复：把 SummarizeService 接入装配图（之前 NewSummarizeService 在
// 生产装配路径零调用点，是 404 行死代码）。本函数仍委托 rca 处理 ent 反查与编排，
// SummarizeService 通过 ai.summarize skill 独立被 SkillRegistry 消费（见 skills.go），
// 二者职责解耦：rca 负责按 ticketID 拉上下文，SummarizeService 负责"按文本 + metadata"摘要。
func (s *Service) SummarizeTicket(ctx context.Context, ticketID int, tenantID int) (interface{}, error) {
	return s.rca.SummarizeTicket(ctx, ticketID, tenantID)
}

func (s *Service) GetAnalysisReport(ctx context.Context, ticketID int, tenantID int) (interface{}, error) {
	return s.rca.GetAnalysisReport(ctx, ticketID, tenantID)
}

// Analytics and Prediction

func (s *Service) GetDeepAnalytics(ctx context.Context, req *dto.DeepAnalyticsRequest, tenantID int) (interface{}, error) {
	return s.analytics.GetDeepAnalytics(ctx, req, tenantID)
}

func (s *Service) GetTrendPrediction(ctx context.Context, req *dto.TrendPredictionRequest, tenantID int) (interface{}, error) {
	// 前端不带 timeRange 时默认为「过去 6 个月 → 今天」。
	// 否则 req.TimeRange[0/1] 会 panic,与 prediction_service.go 行为对齐。
	if len(req.TimeRange) != 2 {
		now := time.Now()
		req.TimeRange = []string{
			now.AddDate(0, -6, 0).Format("2006-01-02"),
			now.Format("2006-01-02"),
		}
	}

	// Try to use AI-Native SLAForecastSkill first
	if s.slaForecastSkill != nil {
		input := &service.ForecastInput{
			TenantID:  tenantID,
			StartDate: parseDate(req.TimeRange[0]),
			EndDate:   parseDate(req.TimeRange[1]),
			Metrics:   []string{req.PredictionType},
		}
		output, err := s.slaForecastSkill.Execute(ctx, input)
		if err == nil {
			// P0-1（2026-09-06 UAT 修复）：slaForecastSkill 的 PredictionPoint 用
			// "predicted" 字段，与 dto.TrendPredictionResponse.Predictions 契约
			// "predictedValue" 不一致。出口做一层转译，保留 SLAForecastSkill 的
			// 全部增强字段（insights / seasonality / trend / anomalyDates），
			// 同时让前端 SLATrendPredictionCard 的 predictedValue 字段拿到真值。
			return normalizeForecastOutput(output), nil
		}
		// Fall back to legacy prediction service on error
		s.logger.Warnw("SLAForecastSkill failed, falling back to legacy", "error", err)
	}
	return s.prediction.GetTrendPrediction(ctx, req, tenantID)
}

// normalizeForecastOutput 把 SLAForecastSkill.ForecastOutput 转译为 dto 契约
// （predictions[i].predictedValue 而非 predicted）。同时保留 SLAForecastSkill
// 的 AI 增强字段（insights/seasonality/trend/anomalyDates）作为额外元数据。
func normalizeForecastOutput(output *service.ForecastOutput) interface{} {
	if output == nil {
		return output
	}
	type predictionDTO struct {
		Date           string  `json:"date"`
		PredictedValue float64 `json:"predictedValue"`
		LowerBound     float64 `json:"lowerBound"`
		UpperBound     float64 `json:"upperBound"`
		Confidence     float64 `json:"confidence"`
		Anomaly        bool    `json:"anomaly"`
	}
	type forecastDTO struct {
		Predictions  []predictionDTO `json:"predictions"`
		Confidence   float64         `json:"confidence"`
		Model        string          `json:"model"`
		GeneratedAt  string          `json:"generatedAt"`
		Insights     string          `json:"insights"`
		Seasonality  map[string]bool `json:"seasonality"`
		Trend        string          `json:"trend"`
		AnomalyDates []string        `json:"anomalyDates"`
	}
	points := make([]predictionDTO, 0, len(output.Predictions))
	for _, p := range output.Predictions {
		points = append(points, predictionDTO{
			Date:           p.Date.Format("2006-01-02"),
			PredictedValue: p.Predicted,
			LowerBound:     p.LowerBound,
			UpperBound:     p.UpperBound,
			Confidence:     p.Confidence,
			Anomaly:        p.Anomaly,
		})
	}
	return forecastDTO{
		Predictions:  points,
		Confidence:   output.Confidence,
		Model:        output.Model,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Insights:     output.Insights,
		Seasonality:  output.Seasonality,
		Trend:        output.Trend,
		AnomalyDates: output.AnomalyDates,
	}
}

// GetForecastInsights returns AI-generated insights for SLA forecasting
func (s *Service) GetForecastInsights(ctx context.Context, req *dto.TrendPredictionRequest, tenantID int) (interface{}, error) {
	if s.slaForecastSkill == nil {
		return nil, fmt.Errorf("SLAForecastSkill not initialized")
	}

	input := &service.ForecastInput{
		TenantID:  tenantID,
		StartDate: parseDate(req.TimeRange[0]),
		EndDate:   parseDate(req.TimeRange[1]),
		Metrics:   []string{req.PredictionType},
	}

	output, err := s.slaForecastSkill.Execute(ctx, input)
	if err != nil {
		return nil, err
	}

	// Return insights-focused response
	return map[string]interface{}{
		"confidence":    output.Confidence,
		"model":         output.Model,
		"insights":      output.Insights,
		"seasonality":   output.Seasonality,
		"trend":         output.Trend,
		"anomaly_dates": output.AnomalyDates,
	}, nil
}

// Telemetry

func (s *Service) SaveFeedback(ctx context.Context, tenantID, userID int, requestID, kind, query, itemType string, itemID *int, useful bool, score *int, notes *string) error {
	if s.aiTelemetryService == nil {
		return fmt.Errorf("AI telemetry service not initialized")
	}
	return s.aiTelemetryService.SaveFeedback(ctx, tenantID, userID, requestID, kind, query, itemType, itemID, useful, score, notes)
}

func (s *Service) GetMetrics(ctx context.Context, tenantID int, lookbackDays int) (interface{}, error) {
	if s.aiTelemetryService == nil {
		return nil, fmt.Errorf("AI telemetry service not initialized")
	}
	return s.aiTelemetryService.GetMetrics(ctx, tenantID, lookbackDays)
}

// SearchKnowledge handles RAG search over knowledge base
func (s *Service) SearchKnowledge(ctx context.Context, tenantID int, query string, searchType string, limit int) (interface{}, error) {
	s.logger.Infow("Knowledge Search", "query", query, "type", searchType, "tenantID", tenantID)

	if s.rag == nil {
		// Fallback to basic search if RAG is not available
		return []map[string]interface{}{}, nil
	}

	results, err := s.rag.Ask(ctx, tenantID, query, limit)
	if err != nil {
		s.logger.Warnw("RAG search failed", "error", err)
		return []map[string]interface{}{}, nil
	}

	return results, nil
}

// TriageTicket provides ticket classification and recommendations using LLM
func (s *Service) TriageTicket(ctx context.Context, tenantID int, title, description, category, priority string) (interface{}, error) {
	s.logger.Infow("Ticket Triage with LLM", "title", title, "tenantID", tenantID)

	// Use LLM-powered TriageService if available
	if s.triageService != nil {
		result := s.triageService.Suggest(ctx, title, description)
		return map[string]interface{}{
			"title":       title,
			"description": description,
			"suggestions": map[string]interface{}{
				"category":   result.Category,
				"priority":   result.Priority,
				"confidence": result.Confidence,
				"reasoning":  result.Explanation,
				"urgency":    s.determineUrgency(result.Priority),
			},
		}, nil
	}

	// Fallback to keyword-based classification
	result := map[string]interface{}{
		"title":       title,
		"description": description,
		"suggestions": make(map[string]interface{}),
	}

	suggestedCategory := category
	suggestedPriority := priority
	suggestedUrgency := "medium"

	titleLower := title
	if len(titleLower) > 0 {
		switch {
		case containsAny(titleLower, "网络", "网速", "连接", "wifi", "网络"):
			suggestedCategory = "network"
		case containsAny(titleLower, "软件", "应用", "系统", "程序", "app"):
			suggestedCategory = "software"
		case containsAny(titleLower, "硬件", "电脑", "设备", "服务器", "hardware"):
			suggestedCategory = "hardware"
		case containsAny(titleLower, "账号", "密码", "权限", "登录", "access"):
			suggestedCategory = "access"
		case containsAny(titleLower, "打印机", "打印", "print"):
			suggestedCategory = "printer"
		case containsAny(titleLower, "邮箱", "邮件", "email", "outlook"):
			suggestedCategory = "email"
		default:
			suggestedCategory = "general"
		}
	}

	descLower := description
	if containsAny(descLower, "紧急", "严重", "无法工作", "critical", "urgent", "emergency") {
		suggestedPriority = "critical"
		suggestedUrgency = "high"
	} else if containsAny(descLower, "重要", "影响工作", "high", "important") {
		suggestedPriority = "high"
		suggestedUrgency = "high"
	} else if containsAny(descLower, "不紧急", "low", "minor") {
		suggestedPriority = "low"
		suggestedUrgency = "low"
	}

	suggestions := make(map[string]interface{})
	suggestions["category"] = suggestedCategory
	suggestions["priority"] = suggestedPriority
	suggestions["urgency"] = suggestedUrgency
	suggestions["confidence"] = 0.7
	suggestions["reasoning"] = "Based on keyword analysis"

	result["suggestions"] = suggestions

	return result, nil
}

func (s *Service) determineUrgency(priority string) string {
	switch priority {
	case "critical":
		return "high"
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

// Evaluate delegates to AITelemetryService.Evaluate (nil-safe for unit tests).
func (s *Service) Evaluate(ctx context.Context, tenantID int, days int) (*service.AIEvaluationReport, error) {
	if s.aiTelemetryService == nil {
		return &service.AIEvaluationReport{GeneratedAt: time.Now().Format(time.RFC3339), LookbackDays: days}, nil
	}
	return s.aiTelemetryService.Evaluate(ctx, tenantID, days)
}

// ListAuditLogs delegates to AITelemetryService.ListAuditLogs (nil-safe for unit tests).
func (s *Service) ListAuditLogs(ctx context.Context, tenantID, page, pageSize int, kind string, days int) ([]service.AIAuditEntry, int, error) {
	if s.aiTelemetryService == nil {
		return []service.AIAuditEntry{}, 0, nil
	}
	return s.aiTelemetryService.ListAuditLogs(ctx, tenantID, page, pageSize, kind, days)
}

// containsAny checks if string contains any of the keywords
func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if len(s) >= len(kw) {
			for i := 0; i <= len(s)-len(kw); i++ {
				if len(s[i:i+len(kw)]) >= len(kw) {
					// Simple case-insensitive check
					sub := ""
					for j := 0; j < len(kw); j++ {
						if i+j < len(s) {
							c := s[i+j]
							if c >= 'A' && c <= 'Z' {
								c = c + 32
							}
							sub += string(c)
						}
					}
					if sub == kw {
						return true
					}
				}
			}
		}
	}
	return false
}

// parseDate parses date string in YYYY-MM-DD format
func parseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Now()
	}
	return t
}

// persistAIResult serializes and saves an AI analysis result to the database.
// Errors are logged but not propagated — analysis results should still be returned
// to the caller even if persistence fails.
func (s *Service) persistAIResult(ctx context.Context, analysisType string, tenantID, userID int, prompt string, result interface{}, model string, latencyMs, totalTokens int, confidenceScore *float64) {
	if s.repo == nil {
		return
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		s.logger.Warnw("persistAIResult: marshal failed", "error", err)
		return
	}
	record := &AIAnalysisResult{
		TenantID:      tenantID,
		UserID:        userID,
		AnalysisType:  analysisType,
		RequestPrompt: prompt,
		ResultJSON:    string(resultJSON),
		Model:         model,
		Degraded:      false,
	}
	if latencyMs > 0 {
		record.LatencyMs = latencyMs
	}
	if totalTokens > 0 {
		record.TotalTokens = totalTokens
	}
	if confidenceScore != nil {
		record.ConfidenceScore = *confidenceScore
	}
	_, err = s.repo.SaveAIAnalysisResult(ctx, record)
	if err != nil {
		s.logger.Warnw("persistAIResult: save failed", "error", err)
	}
}

// ListAIAnalysisResults returns analysis history for the tenant.
func (s *Service) ListAIAnalysisResults(ctx context.Context, tenantID int, analysisType string, limit int) ([]*AIAnalysisResult, error) {
	return s.repo.ListAIAnalysisResults(ctx, tenantID, analysisType, limit)
}

// GetAIAnalysisResult returns a single analysis result by ID.
func (s *Service) GetAIAnalysisResult(ctx context.Context, id int, tenantID int) (*AIAnalysisResult, error) {
	return s.repo.GetAIAnalysisResult(ctx, id, tenantID)
}

// DeleteAIAnalysisResult deletes an analysis result record.
func (s *Service) DeleteAIAnalysisResult(ctx context.Context, id int, tenantID int) error {
	return s.repo.DeleteAIAnalysisResult(ctx, id, tenantID)
}
