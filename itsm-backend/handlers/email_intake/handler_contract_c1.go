package email_intake

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"itsm-backend/common"
)

func contractPathID(c *gin.Context, name string) (int, bool) {
	value, err := strconv.Atoi(c.Param(name))
	if err != nil || value < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid "+name)
		return 0, false
	}
	return value, true
}

func (h *Handler) GetSupportContract(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	contract, err := h.svc.supportContractByID(c, tenantID, contractID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	response, err := h.svc.enrichSupportContractResponse(c, tenantID, contract, mapSupportContract(contract))
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, response)
}

func (h *Handler) ListContractServiceItems(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListContractServiceItems(c, tenantID, contractID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) CreateContractServiceItem(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	var req contractServiceItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid contract service item")
		return
	}
	entity, err := h.svc.CreateContractServiceItem(c, tenantID, actorID(c), contractID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapContractServiceItem(entity, 0))
}

func (h *Handler) UpdateContractServiceItem(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	itemID, ok := contractPathID(c, "itemId")
	if !ok {
		return
	}
	var req contractServiceItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid contract service item")
		return
	}
	entity, err := h.svc.UpdateContractServiceItem(c, tenantID, actorID(c), contractID, itemID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapContractServiceItem(entity, 0))
}

func (h *Handler) DeleteContractServiceItem(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	itemID, ok := contractPathID(c, "itemId")
	if !ok {
		return
	}
	if err := h.svc.DeleteContractServiceItem(c, tenantID, actorID(c), contractID, itemID); err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ListContractAmendments(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListContractAmendments(c, tenantID, contractID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) CreateContractAmendment(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	var req contractAmendmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid contract amendment")
		return
	}
	entity, err := h.svc.CreateContractAmendment(c, tenantID, actorID(c), contractID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapContractAmendment(entity))
}

func (h *Handler) EffectContractAmendment(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	amendmentID, ok := contractPathID(c, "amendmentId")
	if !ok {
		return
	}
	entity, err := h.svc.EffectContractAmendment(c, tenantID, actorID(c), contractID, amendmentID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapContractAmendment(entity))
}

func (h *Handler) ListContractUsage(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListContractUsage(c, tenantID, contractID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) RecordContractUsage(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	var req contractUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid contract usage")
		return
	}
	entity, err := h.svc.RecordContractUsage(c, tenantID, actorID(c), contractID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapContractUsageEntry(entity))
}

func (h *Handler) GetContractBalance(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	balance, err := h.svc.contractBalance(c, tenantID, contractID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, balance)
}

func (h *Handler) ListContractProjects(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListContractProjects(c, tenantID, contractID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListProjectContracts(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	projectID, ok := contractPathID(c, "id")
	if !ok {
		return
	}
	items, err := h.svc.ListProjectContracts(c, tenantID, projectID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) CreateContractProjectLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	var req contractProjectLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid contract project link")
		return
	}
	entity, err := h.svc.CreateContractProjectLink(c, tenantID, actorID(c), contractID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	row := &contractProjectLinkRow{Link: entity}
	common.Success(c, mapContractProjectLink(row))
}

func (h *Handler) DeleteContractProjectLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	linkID, ok := contractPathID(c, "linkId")
	if !ok {
		return
	}
	if err := h.svc.DeleteContractProjectLink(c, tenantID, actorID(c), contractID, linkID); err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) SignSupportContract(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	var req contractSignRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.ParamErrorWithErr(c, err, "invalid contract sign request")
			return
		}
	}
	entity, err := h.svc.SignSupportContract(c, tenantID, actorID(c), contractID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapSupportContract(entity))
}

func (h *Handler) TerminateSupportContractWithReason(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	var req contractTerminateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid contract terminate request")
		return
	}
	entity, err := h.svc.TerminateSupportContractWithReason(c, tenantID, actorID(c), contractID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapSupportContract(entity))
}

func (h *Handler) RenewSupportContract(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	contractID, ok := pathID(c)
	if !ok {
		return
	}
	var req contractRenewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid contract renew request")
		return
	}
	entity, err := h.svc.RenewSupportContract(c, tenantID, actorID(c), contractID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapSupportContract(entity))
}

func respondContractError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrContractNotFound):
		common.NotFound(c, "支持合同不存在")
	case errors.Is(err, ErrContractNoDuplicate):
		common.Fail(c, common.ConflictCode, "合同号在租户内已存在")
	case errors.Is(err, ErrContractStatusInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "合同状态不允许该操作")
	case errors.Is(err, ErrContractAmountImmutable):
		common.Fail(c, common.UnprocessableEntityCode, "已生效合同的金额或有效期已锁定，请通过变更单修改")
	case errors.Is(err, ErrContractCustomerImmutable):
		common.Fail(c, common.UnprocessableEntityCode, "合同已挂项目，不能直接修改客户")
	case errors.Is(err, ErrCustomerInvalidReference):
		common.Fail(c, common.UnprocessableEntityCode, "合同引用的 SLA 或父/续签合同不存在或不属于当前租户")
	case errors.Is(err, ErrContractServiceItemRequired):
		common.Fail(c, common.UnprocessableEntityCode, "签订合同前必须至少有一个有效服务项")
	case errors.Is(err, ErrContractServiceItemInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "服务项字段不合法；议价金额需填写说明")
	case errors.Is(err, ErrContractServiceItemMismatch):
		common.Fail(c, common.UnprocessableEntityCode, "服务项不属于该合同或计量单位不匹配")
	case errors.Is(err, ErrContractAmendmentInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "变更单不合法")
	case errors.Is(err, ErrContractUsageSourceConflict):
		common.Fail(c, common.ConflictCode, "同一计量来源的重复提交载荷不一致")
	case errors.Is(err, ErrContractQuotaExceeded):
		common.Fail(c, common.ConflictCode, "服务项额度不足")
	case errors.Is(err, ErrContractAllocationExceeded):
		common.Fail(c, common.ConflictCode, "项目分配额度超出合同可用额度")
	case errors.Is(err, ErrContractProjectLinkInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "合同项目关联不合法")
	case errors.Is(err, ErrContractProjectLinkNotFound):
		common.NotFound(c, "合同项目关联不存在")
	default:
		common.FailWithErr(c, err, "合同操作失败")
	}
}
