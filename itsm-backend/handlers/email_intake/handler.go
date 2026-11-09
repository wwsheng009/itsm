package email_intake

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"itsm-backend/common"
	"itsm-backend/middleware"
)

type Handler struct {
	svc          *Service
	orchestrator *EmailIntakeOrchestrator
}

func (h *Handler) SetOrchestrator(orchestrator *EmailIntakeOrchestrator) {
	h.orchestrator = orchestrator
}

func NewHandler(service *Service) *Handler {
	return &Handler{svc: service}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	root := rg.Group("/email-intake")
	root.GET("/conversations", middleware.RequirePermission("email_intake", "read"), h.ListConversations)
	// Conversation detail contains customer email content and therefore requires
	// the stronger NOC review permission; list metadata remains readable.
	root.GET("/conversations/:id", middleware.RequirePermission("email_intake", "review"), h.GetConversation)
	root.POST("/conversations/:id/revalidate", middleware.RequirePermission("email_intake", "review"), h.RevalidateConversation)
	root.POST("/conversations/:id/corrections", middleware.RequirePermission("email_intake", "review"), h.CorrectConversation)
	root.POST("/conversations/:id/confirm", middleware.RequirePermission("email_intake", "review"), h.ConfirmConversation)
	root.POST("/conversations/:id/reject", middleware.RequirePermission("email_intake", "review"), h.RejectConversation)
	root.POST("/conversations/:id/retry", middleware.RequirePermission("email_intake", "retry"), h.RetryConversation)
	root.POST("/conversations/:id/override", middleware.RequirePermission("email_intake", "override"), h.OverrideConversation)

	// 客户、分支、来源组织、支持合同与外部合同映射属于运营主数据，
	// 不再只挂在 /email-intake 下。前端已归位到 /master-data；
	// 旧 API 前缀保留为兼容别名，避免已有调用方与历史脚本中断。
	//
	// 注意：这里保留两套显式路由声明，而不是抽 helper——middleware 的路由扫描器
	// 按函数体内的 .Group() 链静态解析完整路径；helper 参数无法被解析，会导致
	// authz precheck 漏掉 master-data 路由（非 super_admin 被 fail-closed 403）。
	customers := root.Group("/customers")
	customers.GET("", middleware.RequirePermission("customer_master", "read"), h.ListCustomers)
	customers.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateCustomer)
	customers.PUT("/:id", middleware.RequirePermission("customer_master", "write"), h.UpdateCustomer)
	customers.DELETE("/:id", middleware.RequirePermission("customer_master", "write"), h.DisableCustomer)
	branches := root.Group("/branches")
	branches.GET("", middleware.RequirePermission("customer_master", "read"), h.ListBranches)
	branches.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateBranch)
	branches.PUT("/:id", middleware.RequirePermission("customer_master", "write"), h.UpdateBranch)
	branches.DELETE("/:id", middleware.RequirePermission("customer_master", "write"), h.DisableBranch)
	sources := root.Group("/source-organizations")
	sources.GET("", middleware.RequirePermission("customer_master", "read"), h.ListSourceOrganizations)
	sources.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateSourceOrganization)
	sources.PUT("/:id", middleware.RequirePermission("customer_master", "write"), h.UpdateSourceOrganization)
	sources.DELETE("/:id", middleware.RequirePermission("customer_master", "write"), h.DisableSourceOrganization)
	contracts := root.Group("/support-contracts")
	contracts.GET("", middleware.RequirePermission("support_contract", "read"), h.ListSupportContracts)
	contracts.POST("", middleware.RequirePermission("support_contract", "write"), h.CreateSupportContract)
	contracts.PUT("/:id", middleware.RequirePermission("support_contract", "write"), h.UpdateSupportContract)
	contracts.DELETE("/:id", middleware.RequirePermission("support_contract", "write"), h.TerminateSupportContract)
	references := root.Group("/external-contract-references")
	references.GET("", middleware.RequirePermission("support_contract", "read"), h.ListExternalContractReferences)
	references.POST("", middleware.RequirePermission("support_contract", "write"), h.CreateExternalContractReference)
	references.PUT("/:id", middleware.RequirePermission("support_contract", "write"), h.UpdateExternalContractReference)
	references.DELETE("/:id", middleware.RequirePermission("support_contract", "write"), h.DeleteExternalContractReference)

	masterData := rg.Group("/master-data")
	masterCustomers := masterData.Group("/customers")
	masterCustomers.GET("", middleware.RequirePermission("customer_master", "read"), h.ListCustomers)
	masterCustomers.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateCustomer)
	masterCustomers.GET("/:id", middleware.RequirePermission("customer_master", "read"), h.GetCustomer)
	masterCustomers.PUT("/:id", middleware.RequirePermission("customer_master", "write"), h.UpdateCustomer)
	masterCustomers.DELETE("/:id", middleware.RequirePermission("customer_master", "write"), h.DisableCustomer)
	masterCustomers.POST("/:id/merge", middleware.RequirePermission("customer_master", "write"), h.MergeCustomer)
	masterCustomers.POST("/:id/merge/undo", middleware.RequirePermission("customer_master", "write"), h.UndoCustomerMerge)
	masterCustomers.GET("/:id/merge-history", middleware.RequirePermission("customer_master", "read"), h.ListCustomerMergeHistory)
	masterCustomers.GET("/:id/360", middleware.RequirePermission("customer_master", "read"), h.GetCustomer360)
	masterCustomerContacts := masterCustomers.Group("/:id/contacts")
	masterCustomerContacts.GET("", middleware.RequirePermission("customer_master", "read"), h.ListCustomerContacts)
	masterCustomerContacts.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateCustomerContact)
	masterCustomerContacts.PUT("/:contactId", middleware.RequirePermission("customer_master", "write"), h.UpdateCustomerContact)
	masterCustomerContacts.DELETE("/:contactId", middleware.RequirePermission("customer_master", "write"), h.DeleteCustomerContact)
	masterCustomerLinks := masterCustomers.Group("/:id/tenant-links")
	masterCustomerLinks.GET("", middleware.RequirePermission("customer_master", "read"), h.ListCustomerTenantLinks)
	masterCustomerLinks.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateCustomerTenantLink)
	masterCustomerLinks.PUT("/:linkId", middleware.RequirePermission("customer_master", "write"), h.UpdateCustomerTenantLink)
	masterCustomerLinks.DELETE("/:linkId", middleware.RequirePermission("customer_master", "write"), h.DeleteCustomerTenantLink)
	masterCustomerExternalIDs := masterCustomers.Group("/:id/external-ids")
	masterCustomerExternalIDs.GET("", middleware.RequirePermission("customer_master", "read"), h.ListCustomerExternalIDs)
	masterCustomerExternalIDs.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateCustomerExternalID)
	masterCustomerExternalIDs.PUT("/:externalId", middleware.RequirePermission("customer_master", "write"), h.UpdateCustomerExternalID)
	masterCustomerExternalIDs.DELETE("/:externalId", middleware.RequirePermission("customer_master", "write"), h.DeleteCustomerExternalID)
	masterCustomerRelations := masterCustomers.Group("/:id/relationships")
	masterCustomerRelations.GET("", middleware.RequirePermission("customer_master", "read"), h.ListCustomerRelations)
	masterCustomerRelations.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateCustomerRelationship)
	masterCustomerRelations.PUT("/:relationId", middleware.RequirePermission("customer_master", "write"), h.UpdateCustomerRelationship)
	masterCustomerRelations.DELETE("/:relationId", middleware.RequirePermission("customer_master", "write"), h.DeleteCustomerRelationship)
	masterCustomers.GET("/:id/relations-tree", middleware.RequirePermission("customer_master", "read"), h.GetCustomerRelationsTree)
	masterCustomerProjects := masterCustomers.Group("/:id/projects")
	masterCustomerProjects.GET("", middleware.RequirePermission("customer_master", "read"), h.ListCustomerProjects)
	masterCustomerProjects.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateCustomerProjectLink)
	masterCustomerProjects.DELETE("/:linkId", middleware.RequirePermission("customer_master", "write"), h.DeleteCustomerProjectLink)
	masterBranches := masterData.Group("/branches")
	masterBranches.GET("", middleware.RequirePermission("customer_master", "read"), h.ListBranches)
	masterBranches.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateBranch)
	masterBranches.PUT("/:id", middleware.RequirePermission("customer_master", "write"), h.UpdateBranch)
	masterBranches.DELETE("/:id", middleware.RequirePermission("customer_master", "write"), h.DisableBranch)
	masterSources := masterData.Group("/source-organizations")
	masterSources.GET("", middleware.RequirePermission("customer_master", "read"), h.ListSourceOrganizations)
	masterSources.POST("", middleware.RequirePermission("customer_master", "write"), h.CreateSourceOrganization)
	masterSources.PUT("/:id", middleware.RequirePermission("customer_master", "write"), h.UpdateSourceOrganization)
	masterSources.DELETE("/:id", middleware.RequirePermission("customer_master", "write"), h.DisableSourceOrganization)
	masterContracts := masterData.Group("/support-contracts")
	masterContracts.GET("", middleware.RequirePermission("support_contract", "read"), h.ListSupportContracts)
	masterContracts.POST("", middleware.RequirePermission("support_contract", "write"), h.CreateSupportContract)
	masterContracts.GET("/:id", middleware.RequirePermission("support_contract", "read"), h.GetSupportContract)
	masterContracts.PUT("/:id", middleware.RequirePermission("support_contract", "write"), h.UpdateSupportContract)
	masterContracts.DELETE("/:id", middleware.RequirePermission("support_contract", "write"), h.TerminateSupportContract)
	masterContracts.POST("/:id/sign", middleware.RequirePermission("support_contract", "write"), h.SignSupportContract)
	masterContracts.POST("/:id/terminate", middleware.RequirePermission("support_contract", "write"), h.TerminateSupportContractWithReason)
	masterContracts.POST("/:id/renew", middleware.RequirePermission("support_contract", "write"), h.RenewSupportContract)
	masterContracts.GET("/:id/items", middleware.RequirePermission("support_contract", "read"), h.ListContractServiceItems)
	masterContracts.POST("/:id/items", middleware.RequirePermission("support_contract", "write"), h.CreateContractServiceItem)
	masterContracts.PUT("/:id/items/:itemId", middleware.RequirePermission("support_contract", "write"), h.UpdateContractServiceItem)
	masterContracts.DELETE("/:id/items/:itemId", middleware.RequirePermission("support_contract", "write"), h.DeleteContractServiceItem)
	masterContracts.GET("/:id/amendments", middleware.RequirePermission("support_contract", "read"), h.ListContractAmendments)
	masterContracts.POST("/:id/amendments", middleware.RequirePermission("support_contract", "write"), h.CreateContractAmendment)
	masterContracts.POST("/:id/amendments/:amendmentId/effect", middleware.RequirePermission("support_contract", "write"), h.EffectContractAmendment)
	masterContracts.GET("/:id/usage", middleware.RequirePermission("support_contract", "read"), h.ListContractUsage)
	masterContracts.POST("/:id/usage", middleware.RequirePermission("support_contract", "write"), h.RecordContractUsage)
	masterContracts.GET("/:id/balance", middleware.RequirePermission("support_contract", "read"), h.GetContractBalance)
	masterContracts.GET("/:id/projects", middleware.RequirePermission("support_contract", "read"), h.ListContractProjects)
	masterContracts.POST("/:id/projects", middleware.RequirePermission("support_contract", "write"), h.CreateContractProjectLink)
	masterContracts.DELETE("/:id/projects/:linkId", middleware.RequirePermission("support_contract", "write"), h.DeleteContractProjectLink)
	masterReferences := masterData.Group("/external-contract-references")
	masterReferences.GET("", middleware.RequirePermission("support_contract", "read"), h.ListExternalContractReferences)
	masterReferences.POST("", middleware.RequirePermission("support_contract", "write"), h.CreateExternalContractReference)
	masterReferences.PUT("/:id", middleware.RequirePermission("support_contract", "write"), h.UpdateExternalContractReference)
	masterReferences.DELETE("/:id", middleware.RequirePermission("support_contract", "write"), h.DeleteExternalContractReference)

	// Business-record lookup: authenticated tenant users need id/name/code to
	// select a customer on tickets/problems/knowledge/etc. This is deliberately
	// separate from /master-data/customers, whose broader read permission stays
	// restricted to master-data maintainers.
	lookups := rg.Group("/lookups")
	lookups.GET("/customers", h.ListCustomerOptions)

	projectLinks := rg.Group("/projects")
	projectLinks.GET("/:id/customer-links", middleware.RequirePermission("project", "read"), h.ListProjectCustomerLinks)
	projectLinks.POST("/:id/customer-links", middleware.RequirePermission("project", "write"), h.CreateProjectCustomerLink)
	projectLinks.DELETE("/:id/customer-links/:linkId", middleware.RequirePermission("project", "write"), h.DeleteProjectCustomerLink)
	projectLinks.GET("/:id/contract-links", middleware.RequirePermission("support_contract", "read"), h.ListProjectContracts)

	onCall := root.Group("/on-call")
	onCall.GET("/schedules", middleware.RequirePermission("on_call", "read"), h.ListSchedules)
	onCall.POST("/schedules", middleware.RequirePermission("on_call", "write"), h.CreateSchedule)
	onCall.POST("/shifts", middleware.RequirePermission("on_call", "write"), h.CreateShift)
	onCall.GET("/shifts", middleware.RequirePermission("on_call", "read"), h.ListShifts)
	onCall.PUT("/shifts/:id", middleware.RequirePermission("on_call", "write"), h.UpdateShift)
	onCall.DELETE("/shifts/:id", middleware.RequirePermission("on_call", "write"), h.DeleteShift)
	onCall.GET("/current", middleware.RequirePermission("on_call", "read"), h.CurrentOnCall)
}

type customerRequest struct {
	Name                   string         `json:"name" binding:"required"`
	Code                   string         `json:"code"`
	ShortName              string         `json:"shortName"`
	LegalName              string         `json:"legalName"`
	EnName                 string         `json:"enName"`
	Aliases                []string       `json:"aliases"`
	HistoricalNames        []string       `json:"historicalNames"`
	TaxID                  string         `json:"taxId"`
	DunsNumber             string         `json:"dunsNumber"`
	RegistrationNo         string         `json:"registrationNo"`
	EmailDomains           []string       `json:"emailDomains"`
	CustomerType           string         `json:"customerType"`
	Industry               string         `json:"industry"`
	SubIndustry            string         `json:"subIndustry"`
	SizeClass              string         `json:"sizeClass"`
	OwnershipType          string         `json:"ownershipType"`
	Tier                   string         `json:"tier"`
	VIPLevel               int16          `json:"vipLevel"`
	CreditRating           string         `json:"creditRating"`
	Website                string         `json:"website"`
	MainPhone              string         `json:"mainPhone"`
	ServiceEmail           string         `json:"serviceEmail"`
	Address                map[string]any `json:"address"`
	Timezone               string         `json:"timezone"`
	Language               string         `json:"language"`
	ServiceTier            string         `json:"serviceTier"`
	SLADefinitionID        *int           `json:"slaDefinitionId"`
	SupportHours           string         `json:"supportHours"`
	CoverageScope          map[string]any `json:"coverageScope"`
	AccountManagerID       *int           `json:"accountManagerId"`
	ServiceOwnerID         *int           `json:"serviceOwnerId"`
	CSManagerID            *int           `json:"csManagerId"`
	OncallGroupID          *int           `json:"oncallGroupId"`
	BillingCurrency        string         `json:"billingCurrency"`
	PaymentTerms           string         `json:"paymentTerms"`
	CreditLimit            *float64       `json:"creditLimit"`
	InvoiceTitle           string         `json:"invoiceTitle"`
	BillingAddress         map[string]any `json:"billingAddress"`
	LifecycleStage         string         `json:"lifecycleStage"`
	OnboardedAt            *time.Time     `json:"onboardedAt"`
	ContractStartAt        *time.Time     `json:"contractStartAt"`
	ContractEndAt          *time.Time     `json:"contractEndAt"`
	ChurnAt                *time.Time     `json:"churnAt"`
	ChurnReason            string         `json:"churnReason"`
	DataSource             string         `json:"dataSource"`
	Status                 string         `json:"status"`
	LinkedCustomerTenantID *int           `json:"linkedCustomerTenantId"`
	Version                *int64         `json:"version"`
}

func (h *Handler) CreateCustomer(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	var req customerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer")
		return
	}
	entity, err := h.svc.CreateCustomerMaster(c, tenantID, actorID(c), &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomer(entity))
}

func (h *Handler) ListCustomers(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	page, pageSize := normalizePage(queryInt(c, "page", 0), queryInt(c, "pageSize", 0))
	filters := CustomerListFilters{
		Keyword:        c.Query("keyword"),
		Tier:           c.Query("tier"),
		Status:         c.Query("status"),
		LifecycleStage: c.Query("lifecycleStage"),
		Industry:       c.Query("industry"),
		SortBy:         c.Query("sortBy"),
		SortOrder:      c.Query("sortOrder"),
		Page:           page,
		PageSize:       pageSize,
	}
	if value := queryInt(c, "accountManagerId", 0); value > 0 {
		filters.AccountManagerID = &value
	}
	if value, ok := queryBool(c, "hasTenantLink"); ok {
		filters.HasTenantLink = &value
	}
	if value := queryInt(c, "qualityBelow", 0); value > 0 {
		v := int16(value)
		filters.QualityBelow = &v
	}
	items, total, err := h.svc.ListCustomers(c, tenantID, filters)
	if err != nil {
		common.FailWithErr(c, err, "加载客户主数据失败")
		return
	}
	result := make([]customerResponse, 0, len(items))
	for _, item := range items {
		response := mapCustomer(item.Customer)
		response.BranchCount = item.BranchCount
		response.ContractCount = item.ContractCount
		response.ContactCount = item.ContactCount
		response.TenantLinkCount = item.TenantLinkCount
		response.AccountManagerName = item.AccountManagerName
		result = append(result, response)
	}
	totalPages := 0
	if pageSize > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	common.Success(c, gin.H{
		"items":      result,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

func (h *Handler) ListCustomerOptions(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	options, err := h.svc.ListCustomerOptions(c, tenantID, c.Query("keyword"))
	if err != nil {
		common.FailWithErr(c, err, "加载客户列表失败")
		return
	}
	common.Success(c, gin.H{"items": options})
}

func (h *Handler) GetCustomer(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	detail, err := h.svc.GetCustomerDetail(c, tenantID, id)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	response := mapCustomer(detail.Customer)
	response.BranchCount = detail.BranchCount
	response.ContractCount = detail.ContractCount
	response.ContactCount = detail.ContactCount
	response.TenantLinkCount = detail.TenantLinkCount
	response.AccountManagerName = detail.AccountManagerName
	common.Success(c, gin.H{
		"customer":    response,
		"branches":    mapBranches(detail.Branches),
		"contacts":    mapCustomerContacts(detail.Contacts),
		"tenantLinks": mapCustomerTenantLinks(detail.TenantLinks),
		"externalIds": mapCustomerExternalIDs(detail.ExternalIDs),
	})
}

func (h *Handler) GetCustomer360(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	data, err := h.svc.GetCustomer360(c, tenantID, customerID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, data)
}

func (h *Handler) UpdateCustomer(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req customerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer")
		return
	}
	entity, err := h.svc.UpdateCustomerMaster(c, tenantID, actorID(c), id, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomer(entity))
}

func (h *Handler) DisableCustomer(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	entity, err := h.svc.DisableCustomerMaster(c, tenantID, actorID(c), id)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomer(entity))
}

func (h *Handler) ListCustomerContacts(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListCustomerContacts(c, tenantID, customerID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"items": mapCustomerContacts(items), "total": len(items)})
}

func (h *Handler) CreateCustomerContact(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	var req customerContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer contact")
		return
	}
	entity, err := h.svc.CreateCustomerContact(c, tenantID, customerID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomerContact(entity))
}

func (h *Handler) UpdateCustomerContact(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	contactID, err := strconv.Atoi(c.Param("contactId"))
	if err != nil || contactID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid contact id")
		return
	}
	var req customerContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer contact")
		return
	}
	entity, err := h.svc.UpdateCustomerContact(c, tenantID, customerID, contactID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomerContact(entity))
}

func (h *Handler) DeleteCustomerContact(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	contactID, err := strconv.Atoi(c.Param("contactId"))
	if err != nil || contactID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid contact id")
		return
	}
	if err := h.svc.DeleteCustomerContact(c, tenantID, customerID, contactID); err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ListCustomerTenantLinks(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListCustomerTenantLinks(c, tenantID, customerID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"items": mapCustomerTenantLinks(items), "total": len(items)})
}

func (h *Handler) CreateCustomerTenantLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	var req customerTenantLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer tenant link")
		return
	}
	entity, err := h.svc.CreateCustomerTenantLink(c, tenantID, customerID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomerTenantLink(entity))
}

func (h *Handler) UpdateCustomerTenantLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	linkID, err := strconv.Atoi(c.Param("linkId"))
	if err != nil || linkID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid tenant link id")
		return
	}
	var req customerTenantLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer tenant link")
		return
	}
	entity, err := h.svc.UpdateCustomerTenantLink(c, tenantID, customerID, linkID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomerTenantLink(entity))
}

func (h *Handler) DeleteCustomerTenantLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	linkID, err := strconv.Atoi(c.Param("linkId"))
	if err != nil || linkID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid tenant link id")
		return
	}
	if err := h.svc.DeleteCustomerTenantLink(c, tenantID, customerID, linkID); err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ListCustomerExternalIDs(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListCustomerExternalIDs(c, tenantID, customerID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"items": mapCustomerExternalIDs(items), "total": len(items)})
}

func (h *Handler) CreateCustomerExternalID(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	var req customerExternalIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer external id")
		return
	}
	entity, err := h.svc.CreateCustomerExternalID(c, tenantID, customerID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomerExternalID(entity))
}

func (h *Handler) UpdateCustomerExternalID(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	externalID, err := strconv.Atoi(c.Param("externalId"))
	if err != nil || externalID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid external id")
		return
	}
	var req customerExternalIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer external id")
		return
	}
	entity, err := h.svc.UpdateCustomerExternalID(c, tenantID, customerID, externalID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomerExternalID(entity))
}

func (h *Handler) DeleteCustomerExternalID(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	externalID, err := strconv.Atoi(c.Param("externalId"))
	if err != nil || externalID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid external id")
		return
	}
	if err := h.svc.DeleteCustomerExternalID(c, tenantID, customerID, externalID); err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ListCustomerRelations(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListCustomerRelations(c, tenantID, customerID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	result := make([]customerRelationshipResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapCustomerRelationship(item))
	}
	common.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) CreateCustomerRelationship(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	var req customerRelationshipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer relationship")
		return
	}
	entity, err := h.svc.CreateCustomerRelationship(c, tenantID, actorID(c), customerID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"id": entity.ID})
}

func (h *Handler) UpdateCustomerRelationship(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	relationID, err := strconv.Atoi(c.Param("relationId"))
	if err != nil || relationID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid relationship id")
		return
	}
	var req customerRelationshipUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer relationship")
		return
	}
	entity, err := h.svc.UpdateCustomerRelationship(c, tenantID, customerID, relationID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"id": entity.ID})
}

func (h *Handler) DeleteCustomerRelationship(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	relationID, err := strconv.Atoi(c.Param("relationId"))
	if err != nil || relationID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid relationship id")
		return
	}
	if err := h.svc.DeleteCustomerRelationship(c, tenantID, customerID, relationID); err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) GetCustomerRelationsTree(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	tree, err := h.svc.GetCustomerRelationsTree(c, tenantID, customerID, queryInt(c, "depth", 2))
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapCustomerRelationsTree(tree))
}

func (h *Handler) ListCustomerProjects(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListCustomerProjects(c, tenantID, customerID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"items": mapProjectCustomerLinks(items), "total": len(items)})
}

func (h *Handler) CreateCustomerProjectLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	var req projectCustomerLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid project customer link")
		return
	}
	entity, err := h.svc.CreateCustomerProjectLink(c, tenantID, actorID(c), customerID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapProjectCustomerLink(&projectCustomerLinkRow{Link: entity}))
}

func (h *Handler) DeleteCustomerProjectLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	linkID, err := strconv.Atoi(c.Param("linkId"))
	if err != nil || linkID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid project link id")
		return
	}
	if err := h.svc.DeleteCustomerProjectLink(c, tenantID, customerID, linkID); err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) ListProjectCustomerLinks(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid project id")
		return
	}
	items, err := h.svc.ListProjectCustomerLinks(c, tenantID, projectID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"items": mapProjectCustomerLinks(items), "total": len(items)})
}

func (h *Handler) CreateProjectCustomerLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid project id")
		return
	}
	var req projectCustomerLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid project customer link")
		return
	}
	entity, err := h.svc.CreateProjectCustomerLink(c, tenantID, actorID(c), projectID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, mapProjectCustomerLink(&projectCustomerLinkRow{Link: entity}))
}

func (h *Handler) DeleteProjectCustomerLink(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid project id")
		return
	}
	linkID, err := strconv.Atoi(c.Param("linkId"))
	if err != nil || linkID < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid project link id")
		return
	}
	if err := h.svc.DeleteProjectCustomerLink(c, tenantID, projectID, linkID); err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) MergeCustomer(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	sourceID, ok := pathID(c)
	if !ok {
		return
	}
	var req customerMergeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid customer merge")
		return
	}
	report, err := h.svc.MergeCustomers(c, tenantID, actorID(c), sourceID, &req)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, report)
}

func (h *Handler) UndoCustomerMerge(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	sourceID, ok := pathID(c)
	if !ok {
		return
	}
	report, err := h.svc.UndoCustomerMerge(c, tenantID, actorID(c), sourceID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, report)
}

func (h *Handler) ListCustomerMergeHistory(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, ok := pathID(c)
	if !ok {
		return
	}
	items, err := h.svc.ListCustomerMergeHistory(c, tenantID, customerID)
	if err != nil {
		respondCustomerError(c, err)
		return
	}
	common.Success(c, gin.H{"items": items, "total": len(items)})
}

type branchRequest struct {
	CustomerID     int            `json:"customerId" binding:"required"`
	Code           string         `json:"code"`
	Name           string         `json:"name" binding:"required"`
	BranchType     string         `json:"branchType"`
	Aliases        []string       `json:"aliases"`
	Address        map[string]any `json:"address"`
	Timezone       string         `json:"timezone"`
	ContactPhone   string         `json:"contactPhone"`
	EmailDomains   []string       `json:"emailDomains"`
	ServiceTier    string         `json:"serviceTier"`
	SupportHours   string         `json:"supportHours"`
	IsPrimary      bool           `json:"isPrimary"`
	LinkedTenantID *int           `json:"linkedTenantId"`
	Status         string         `json:"status"`
}

func (h *Handler) CreateBranch(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	var req branchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid branch")
		return
	}
	if exists, err := h.svc.CustomerExists(c, tenantID, req.CustomerID); err != nil || !exists {
		common.Fail(c, common.ParamErrorCode, "customer not found")
		return
	}
	entity, err := h.svc.CreateBranch(c, tenantID, &req)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapBranch(entity))
}

func (h *Handler) ListBranches(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	customerID, _ := strconv.Atoi(c.Query("customerId"))
	items, err := h.svc.ListBranches(c, tenantID, customerID)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	result := make([]branchResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapBranch(item))
	}
	common.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) UpdateBranch(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req branchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid branch")
		return
	}
	if exists, err := h.svc.CustomerExists(c, tenantID, req.CustomerID); err != nil || !exists {
		common.Fail(c, common.ParamErrorCode, "customer not found")
		return
	}
	entity, err := h.svc.UpdateBranch(c, tenantID, id, &req)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapBranch(entity))
}

func (h *Handler) DisableBranch(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	entity, err := h.svc.DisableBranch(c, tenantID, id)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapBranch(entity))
}

type sourceOrganizationRequest struct {
	Name           string   `json:"name" binding:"required"`
	EmailAddresses []string `json:"emailAddresses"`
	EmailDomains   []string `json:"emailDomains"`
	Status         string   `json:"status"`
}

func (h *Handler) CreateSourceOrganization(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	var req sourceOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid source organization")
		return
	}
	entity, err := h.svc.CreateSourceOrganization(c, tenantID, &req)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapSourceOrganization(entity))
}

func (h *Handler) ListSourceOrganizations(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	items, err := h.svc.ListSourceOrganizations(c, tenantID)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	result := make([]sourceOrganizationResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapSourceOrganization(item))
	}
	common.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) UpdateSourceOrganization(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req sourceOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid source organization")
		return
	}
	entity, err := h.svc.UpdateSourceOrganization(c, tenantID, id, &req)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapSourceOrganization(entity))
}

func (h *Handler) DisableSourceOrganization(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	entity, err := h.svc.DisableSourceOrganization(c, tenantID, id)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapSourceOrganization(entity))
}

type supportContractRequest struct {
	CustomerID          int        `json:"customerId" binding:"required"`
	BranchID            *int       `json:"branchId"`
	ContractNumber      string     `json:"contractNumber" binding:"required"`
	ContractName        string     `json:"contractName"`
	ContractType        string     `json:"contractType"`
	Status              string     `json:"status"`
	StartAt             *time.Time `json:"startAt"`
	EndAt               *time.Time `json:"endAt"`
	EffectiveFrom       *time.Time `json:"effectiveFrom"`
	EffectiveUntil      *time.Time `json:"effectiveUntil"`
	SignedAt            *time.Time `json:"signedAt"`
	Amount              *float64   `json:"amount"`
	Currency            string     `json:"currency"`
	ParentContractID    *int       `json:"parentContractId"`
	RenewalOfContractID *int       `json:"renewalOfContractId"`
	SLADefinitionID     *int       `json:"slaDefinitionId"`
	AutoRenew           bool       `json:"autoRenew"`
	TerminatedAt        *time.Time `json:"terminatedAt"`
	TerminationReason   string     `json:"terminationReason"`
	Version             *int64     `json:"version"`
}

func (h *Handler) CreateSupportContract(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	var req supportContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid support contract")
		return
	}
	if err := h.ensureContractRefs(c, tenantID, &req); err != nil {
		return
	}
	entity, err := h.svc.CreateSupportContract(c, tenantID, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapSupportContract(entity))
}

func (h *Handler) ListSupportContracts(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	items, err := h.svc.ListSupportContracts(c, tenantID)
	if err != nil {
		respondContractError(c, err)
		return
	}
	result := make([]supportContractResponse, 0, len(items))
	for _, item := range items {
		response, enrichErr := h.svc.enrichSupportContractResponse(c, tenantID, item, mapSupportContract(item))
		if enrichErr != nil {
			respondContractError(c, enrichErr)
			return
		}
		result = append(result, response)
	}
	common.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) UpdateSupportContract(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req supportContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid support contract")
		return
	}
	if err := h.ensureContractRefs(c, tenantID, &req); err != nil {
		return
	}
	entity, err := h.svc.UpdateSupportContract(c, tenantID, id, &req)
	if err != nil {
		respondContractError(c, err)
		return
	}
	response, enrichErr := h.svc.enrichSupportContractResponse(c, tenantID, entity, mapSupportContract(entity))
	if enrichErr != nil {
		respondContractError(c, enrichErr)
		return
	}
	common.Success(c, response)
}

// ensureContractRefs 校验合同引用的 customer/branch 存在且属于当前租户
func (h *Handler) ensureContractRefs(c *gin.Context, tenantID int, req *supportContractRequest) error {
	if exists, err := h.svc.CustomerExists(c, tenantID, req.CustomerID); err != nil || !exists {
		common.Fail(c, common.ParamErrorCode, "customer not found")
		return errMissingRef
	}
	if req.BranchID != nil {
		if exists, err := h.svc.BranchExistsForCustomer(c, tenantID, *req.BranchID, req.CustomerID); err != nil || !exists {
			common.Fail(c, common.ParamErrorCode, "branch not found for customer")
			return errMissingRef
		}
	}
	return nil
}

func (h *Handler) TerminateSupportContract(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	entity, err := h.svc.TerminateSupportContract(c, tenantID, id)
	if err != nil {
		respondContractError(c, err)
		return
	}
	common.Success(c, mapSupportContract(entity))
}

type externalReferenceRequest struct {
	SourceOrganizationID   int    `json:"sourceOrganizationId" binding:"required"`
	SupportContractID      int    `json:"supportContractId" binding:"required"`
	ExternalContractNumber string `json:"externalContractNumber" binding:"required"`
}

var errMissingRef = errors.New("missing reference")

func (h *Handler) CreateExternalContractReference(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	var req externalReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid external reference")
		return
	}
	if exists, err := h.svc.SourceOrganizationExists(c, tenantID, req.SourceOrganizationID); err != nil || !exists {
		common.Fail(c, common.ParamErrorCode, "source organization not found")
		return
	}
	entity, err := h.svc.CreateExternalContractReference(c, tenantID, &req)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapExternalReference(entity))
}

func (h *Handler) ListExternalContractReferences(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	items, err := h.svc.ListExternalContractReferences(c, tenantID)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	result := make([]externalReferenceResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapExternalReference(item))
	}
	common.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) UpdateExternalContractReference(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req externalReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid external reference")
		return
	}
	if exists, err := h.svc.SourceOrganizationExists(c, tenantID, req.SourceOrganizationID); err != nil || !exists {
		common.Fail(c, common.ParamErrorCode, "source organization not found")
		return
	}
	entity, err := h.svc.UpdateExternalContractReference(c, tenantID, id, &req)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapExternalReference(entity))
}

func (h *Handler) DeleteExternalContractReference(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	deleted, err := h.svc.DeleteExternalContractReference(c, tenantID, id)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, gin.H{"deleted": deleted > 0})
}

type scheduleRequest struct {
	GroupID  int    `json:"groupId" binding:"required"`
	Name     string `json:"name" binding:"required"`
	Timezone string `json:"timezone"`
	Status   string `json:"status"`
}

func (h *Handler) CreateSchedule(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	var req scheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid schedule")
		return
	}
	groupExists, err := h.svc.GroupExistsInTenant(c, tenantID, req.GroupID)
	if err != nil || !groupExists {
		common.Fail(c, common.ParamErrorCode, "group not found in tenant")
		return
	}
	entity, err := h.svc.CreateOnCallSchedule(c, tenantID, &req)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapSchedule(entity))
}

func (h *Handler) ListSchedules(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	items, err := h.svc.ListOnCallSchedules(c, tenantID)
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	result := make([]scheduleResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapSchedule(item))
	}
	common.Success(c, gin.H{"items": result, "total": len(result)})
}

type shiftRequest struct {
	ScheduleID int       `json:"scheduleId" binding:"required"`
	UserID     int       `json:"userId" binding:"required"`
	StartAt    time.Time `json:"startAt" binding:"required"`
	EndAt      time.Time `json:"endAt" binding:"required"`
}

func (h *Handler) CreateShift(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	var req shiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid shift")
		return
	}
	entity, err := h.svc.CreateShift(c, tenantID, req.ScheduleID, req.UserID, req.StartAt, req.EndAt)
	if err != nil {
		code := common.ParamErrorCode
		if !errors.Is(err, ErrOverlappingShift) && !errors.Is(err, ErrInvalidShift) {
			code = common.InternalErrorCode
		}
		common.Fail(c, code, err.Error())
		return
	}
	common.Success(c, shiftResponse{ID: entity.ID, ScheduleID: entity.ScheduleID, UserID: entity.UserID, StartAt: entity.StartAt, EndAt: entity.EndAt})
}

func (h *Handler) ListShifts(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	scheduleID, _ := strconv.Atoi(c.Query("scheduleId"))
	items, err := h.svc.ListShifts(c, tenantID, scheduleID)
	if err != nil {
		common.InternalError(c, "failed to list shifts")
		return
	}
	result := make([]shiftResponse, 0, len(items))
	for _, item := range items {
		result = append(result, shiftResponse{ID: item.ID, ScheduleID: item.ScheduleID, UserID: item.UserID, StartAt: item.StartAt, EndAt: item.EndAt})
	}
	common.Success(c, gin.H{"items": result, "total": len(result)})
}

func (h *Handler) UpdateShift(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ParamError(c, "invalid shift id")
		return
	}
	var req shiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid shift")
		return
	}
	entity, err := h.svc.UpdateShift(c, tenantID, id, req.ScheduleID, req.UserID, req.StartAt, req.EndAt)
	if err != nil {
		code := common.ParamErrorCode
		if errors.Is(err, ErrShiftNotFound) {
			code = common.NotFoundCode
		} else if !errors.Is(err, ErrOverlappingShift) && !errors.Is(err, ErrInvalidShift) {
			code = common.InternalErrorCode
		}
		common.Fail(c, code, err.Error())
		return
	}
	common.Success(c, shiftResponse{ID: entity.ID, ScheduleID: entity.ScheduleID, UserID: entity.UserID, StartAt: entity.StartAt, EndAt: entity.EndAt})
}

func (h *Handler) DeleteShift(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ParamError(c, "invalid shift id")
		return
	}
	err = h.svc.DeleteShift(c, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrShiftNotFound) {
			common.NotFound(c, "shift not found")
		} else {
			common.InternalError(c, "failed to delete shift")
		}
		return
	}
	common.Success(c, gin.H{"deleted": true})
}

func (h *Handler) CurrentOnCall(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	groupID, err := strconv.Atoi(c.Query("groupId"))
	if err != nil || groupID < 1 {
		common.Fail(c, common.ParamErrorCode, "groupId is required")
		return
	}
	current, err := h.svc.CurrentOnCall(c, tenantID, groupID, time.Now())
	if err != nil {
		if errors.Is(err, ErrNoOnCall) {
			common.Fail(c, common.NotFoundCode, err.Error())
		} else {
			common.Fail(c, common.InternalErrorCode, err.Error())
		}
		return
	}
	common.Success(c, current)
}

func (h *Handler) ListConversations(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	pagination := common.GetPaginationFromQuery(c)
	page, pageSize := pagination.Page, pagination.PageSize
	items, total, err := h.svc.ListConversations(c, tenantID, status, page, pageSize)
	if err != nil {
		common.FailWithErr(c, err, "查询邮件会话列表失败")
		return
	}
	result := make([]conversationResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapConversation(item))
	}
	common.Success(c, gin.H{
		"items":    result,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (h *Handler) GetConversation(c *gin.Context) {
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "invalid conversation id")
		return
	}
	item, err := h.svc.GetConversation(c, tenantID, id)
	if isPersistenceNotFound(err) {
		common.Fail(c, common.NotFoundCode, "conversation not found")
		return
	}
	if err != nil {
		common.Fail(c, common.InternalErrorCode, err.Error())
		return
	}
	common.Success(c, mapConversationDetail(item))
}

type conversationVersionRequest struct {
	Version int `json:"version" binding:"required,min=1"`
}
type correctionRequest struct {
	Version int          `json:"version" binding:"required,min=1"`
	Fields  IntakeFields `json:"fields" binding:"required"`
}
type overrideRequest struct {
	Version   int    `json:"version" binding:"required,min=1"`
	Reason    string `json:"reason" binding:"required,min=5,max=1000"`
	Confirmed bool   `json:"confirmed" binding:"required"`
}

func (h *Handler) RevalidateConversation(c *gin.Context) {
	tenantID, id, version, ok := h.conversationAction(c)
	if !ok {
		return
	}
	updated, err := h.orchestrator.Revalidate(c, tenantID, id, version)
	h.actionResult(c, updatedIdentity(updated), err)
}

func (h *Handler) ConfirmConversation(c *gin.Context) {
	tenantID, id, version, ok := h.conversationAction(c)
	if !ok {
		return
	}
	updated, err := h.orchestrator.Confirm(c, tenantID, id, version)
	h.actionResult(c, updatedIdentity(updated), err)
}

func (h *Handler) RetryConversation(c *gin.Context) {
	tenantID, id, version, ok := h.conversationAction(c)
	if !ok {
		return
	}
	updated, err := h.orchestrator.Retry(c, tenantID, id, version)
	h.actionResult(c, updatedIdentity(updated), err)
}

func (h *Handler) RejectConversation(c *gin.Context) {
	tenantID, id, version, ok := h.conversationAction(c)
	if !ok {
		return
	}
	updated, err := h.orchestrator.Reject(c, tenantID, id, version)
	h.actionResult(c, updatedIdentity(updated), err)
}

func (h *Handler) CorrectConversation(c *gin.Context) {
	if h.orchestrator == nil {
		common.Fail(c, common.InternalErrorCode, "email intake orchestrator is unavailable")
		return
	}
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "invalid conversation id")
		return
	}
	var req correctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid corrections")
		return
	}
	userID, err := middleware.GetUserID(c)
	if err != nil {
		common.Fail(c, common.AuthFailedCode, "user context required")
		return
	}
	updated, err := h.orchestrator.ApplyCorrections(c, tenantID, id, req.Version, userID, req.Fields)
	h.actionResult(c, updatedIdentity(updated), err)
}

func (h *Handler) OverrideConversation(c *gin.Context) {
	if h.orchestrator == nil {
		common.Fail(c, common.InternalErrorCode, "email intake orchestrator is unavailable")
		return
	}
	tenantID, ok := tenant(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "invalid conversation id")
		return
	}
	var req overrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid override request")
		return
	}
	if !req.Confirmed {
		common.ParamError(c, "explicit override confirmation is required")
		return
	}
	actorID, actorErr := middleware.GetUserID(c)
	if actorErr != nil {
		common.Fail(c, common.AuthFailedCode, "user context required")
		return
	}
	updated, err := h.orchestrator.Override(c, tenantID, id, req.Version, actorID, req.Reason)
	h.actionResult(c, updatedIdentity(updated), err)
}

func (h *Handler) conversationAction(c *gin.Context) (int, int, int, bool) {
	if h.orchestrator == nil {
		common.Fail(c, common.InternalErrorCode, "email intake orchestrator is unavailable")
		return 0, 0, 0, false
	}
	tenantID, ok := tenant(c)
	if !ok {
		return 0, 0, 0, false
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.Fail(c, common.ParamErrorCode, "invalid conversation id")
		return 0, 0, 0, false
	}
	var req conversationVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ParamErrorWithErr(c, err, "invalid conversation version")
		return 0, 0, 0, false
	}
	return tenantID, id, req.Version, true
}

func (h *Handler) actionResult(c *gin.Context, updated conversationIdentity, err error) {
	if err != nil {
		common.Fail(c, common.ConflictCode, "email intake action conflicts with the current state")
		return
	}
	item, loadErr := h.svc.ReloadConversation(c, updated.id, updated.tenantID)
	if loadErr != nil {
		common.InternalError(c, "failed to load updated email conversation")
		return
	}
	common.Success(c, mapConversation(item))
}

func tenant(c *gin.Context) (int, bool) {
	id, err := middleware.GetTenantID(c)
	if err != nil || id < 1 {
		common.Fail(c, common.AuthFailedCode, "tenant context required")
		return 0, false
	}
	return id, true
}

func actorID(c *gin.Context) int {
	id, err := middleware.GetUserID(c)
	if err != nil {
		return 0
	}
	return id
}

func respondCustomerError(c *gin.Context, err error) {
	var mergeConflict *CustomerMergeConflictError
	if errors.As(err, &mergeConflict) {
		common.Fail(c, common.ConflictCode, "客户合并冲突："+mergeConflict.Error())
		return
	}
	switch {
	case errors.Is(err, ErrCustomerNotFound):
		common.NotFound(c, "客户不存在")
	case errors.Is(err, ErrCustomerVersionConflict):
		common.Fail(c, common.ConflictCode, "客户档案已被其他人更新，请刷新后重试")
	case errors.Is(err, ErrCustomerDuplicate):
		common.Fail(c, common.ConflictCode, "客户名称、编码或税号已存在")
	case errors.Is(err, ErrCustomerInvalidReference):
		common.Fail(c, common.UnprocessableEntityCode, "客户经理、服务负责人、客户成功、值班组或 SLA 不属于当前租户或不可用")
	case errors.Is(err, ErrCustomerInvalidEnum):
		common.Fail(c, common.UnprocessableEntityCode, "枚举字段取值不合法")
	case errors.Is(err, ErrCustomerTenantLinkInvalid):
		common.Fail(c, common.ParamErrorCode, "关联租户不合法或不属于当前服务商")
	case errors.Is(err, ErrCustomerContactInvalid):
		common.Fail(c, common.ParamErrorCode, "联系人、分支或客户归属不合法")
	case errors.Is(err, ErrCustomerContactDuplicate):
		common.Fail(c, common.ConflictCode, "联系人邮箱或主联系人角色已存在")
	case errors.Is(err, ErrCustomerExternalIDNotFound):
		common.NotFound(c, "客户外部标识不存在")
	case errors.Is(err, ErrCustomerExternalIDDuplicate):
		common.Fail(c, common.ConflictCode, "该外部系统的标识已存在")
	case errors.Is(err, ErrCustomerExternalIDInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "外部系统编码或外部标识不合法")
	case errors.Is(err, ErrCustomerRelationNotFound):
		common.NotFound(c, "客户关系不存在")
	case errors.Is(err, ErrCustomerRelationExists):
		common.Fail(c, common.ConflictCode, "客户关系已存在")
	case errors.Is(err, ErrCustomerRelationCycle):
		common.Fail(c, common.ConflictCode, "客户关系会形成环，已拒绝")
	case errors.Is(err, ErrCustomerRelationInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "客户关系类型、目标客户或有效期不合法")
	case errors.Is(err, ErrProjectCustomerLinkNotFound):
		common.NotFound(c, "项目客户关联不存在")
	case errors.Is(err, ErrProjectCustomerLinkExists):
		common.Fail(c, common.ConflictCode, "该项目已关联该客户的同一关系类型")
	case errors.Is(err, ErrProjectCustomerTypeInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "internal 项目不能关联客户")
	case errors.Is(err, ErrProjectCustomerLinkInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "项目客户关联不合法")
	case errors.Is(err, ErrCustomerMergeInvalid):
		common.Fail(c, common.UnprocessableEntityCode, "客户合并请求不合法")
	case errors.Is(err, ErrCustomerMergeUndoExpired):
		common.Fail(c, common.ConflictCode, "合并已超过 7 天撤销窗口或审计记录不存在")
	case errors.Is(err, ErrCustomerMergeAlreadyUndone):
		common.Fail(c, common.ConflictCode, "客户合并已撤销或当前状态不允许撤销")
	default:
		common.FailWithErr(c, err, "客户主数据操作失败")
	}
}

func queryInt(c *gin.Context, key string, fallback int) int {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func queryBool(c *gin.Context, key string) (bool, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return false, false
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return value, true
}

func pathID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		common.Fail(c, common.ParamErrorCode, "invalid id")
		return 0, false
	}
	return id, true
}
