package service

import "context"

// 本文件定义「服务请求交付完成后」的联动契约。
//
// ## 来源与边界（移植声明）
//
// 移植自上游 `heidsoft/itsm` 的 `service/provisioning_delivery.go`（2026-11 前后，52 行，仅 `context`），
// 移植批次：`docs/plan/upstream-absorption-plan-2026-11-06.md` P2。
// **本批只移植契约声明，未接线**（无实现、无调用方、无路由），且不改动任何本地常量：
// 实测本地 `handlers/cmdb` 与 `service` 包中均无同名 `CIProjection*` 常量/接口，
// 因此这不是重复声明，而是给将来「服务请求交付链」差集评审预留的契约起点。
//
// ## 设计意图（与上游一致）
//
// 交付是对真实基础设施产生副作用的动作：任务落库成功只说明「云上有一台机器」，
// 不代表服务请求的生命周期已经收口。这里用两个回调面让应用层在事务提交后把结果
// 投影到其他域，同时保持依赖方向 —— **service 只声明接口，实现放在各自域**，
// 避免 service 反向依赖 HTTP 域。
//
// 上线前置（另立 PR）：确认 `handlers/cmdb` 侧 CI 投影实现、交付事务提交后的调用点，
// 以及「监听方失败只记日志、不得回滚已发生交付」的语义。

// ProvisioningDeliveryListener 接收交付生命周期通知。
// 通知发生在交付事务提交之后；监听方失败只记日志，不得回滚已发生的交付。
type ProvisioningDeliveryListener interface {
	OnProvisioningStarted(ctx context.Context, tenantID, serviceRequestID, actorUserID int)
	OnProvisioningFinished(ctx context.Context, tenantID, serviceRequestID, actorUserID int, delivered bool, detail string)
}

// CI 投影结果状态。与 handlers/cmdb 的 CIProjection* 常量同值，
// 定义在这里是为了让 service 层不反向依赖 HTTP 域。
const (
	ProvisioningCIProjectionCreated     = "created"
	ProvisioningCIProjectionUpdated     = "updated"
	ProvisioningCIProjectionDeferred    = "deferred"
	ProvisioningCIProjectionSkipped     = "skipped"
	ProvisioningCIProjectionFailed      = "failed"
	ProvisioningCIProjectionUnavailable = "unavailable"
)

// ProvisioningCIProjectionRequest 描述一次已成功的云资源交付。
// Resources 是 Provider 返回的原始键值（实例 ID、地域、账号等）。
type ProvisioningCIProjectionRequest struct {
	TenantID           int
	ServiceRequestID   int
	ProvisioningTaskID int
	ResourceType       string
	Provider           string
	Resources          map[string]string
	Payload            map[string]any
}

// ProvisioningCIProjectionResult 是一次 CI 回写的结果；Reason 面向日志，不面向用户文案。
type ProvisioningCIProjectionResult struct {
	Status string
	CIID   int
	Reason string
}

// ProvisioningCIProjector 把已交付的云资源落成配置项。
// 实现位于 handlers/cmdb，复用发现链路的 CI 类型解析与去重规则。
type ProvisioningCIProjector interface {
	ProjectProvisionedResource(ctx context.Context, req ProvisioningCIProjectionRequest) ProvisioningCIProjectionResult
}
