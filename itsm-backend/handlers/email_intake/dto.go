package email_intake

import (
	"time"

	"itsm-backend/ent"
)

type customerResponse struct {
	ID                     int            `json:"id"`
	Code                   string         `json:"code"`
	Name                   string         `json:"name"`
	NormalizedName         string         `json:"normalizedName"`
	ShortName              string         `json:"shortName"`
	LegalName              string         `json:"legalName"`
	EnName                 string         `json:"enName"`
	Aliases                []string       `json:"aliases"`
	HistoricalNames        []string       `json:"historicalNames"`
	TaxID                  string         `json:"taxId"`
	NormalizedTaxID        string         `json:"normalizedTaxId"`
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
	SLADefinitionID        *int           `json:"slaDefinitionId,omitempty"`
	SupportHours           string         `json:"supportHours"`
	CoverageScope          map[string]any `json:"coverageScope"`
	AccountManagerID       *int           `json:"accountManagerId,omitempty"`
	AccountManagerName     string         `json:"accountManagerName,omitempty"`
	ServiceOwnerID         *int           `json:"serviceOwnerId,omitempty"`
	CSManagerID            *int           `json:"csManagerId,omitempty"`
	OncallGroupID          *int           `json:"oncallGroupId,omitempty"`
	BillingCurrency        string         `json:"billingCurrency"`
	PaymentTerms           string         `json:"paymentTerms"`
	CreditLimit            *float64       `json:"creditLimit,omitempty"`
	InvoiceTitle           string         `json:"invoiceTitle"`
	BillingAddress         map[string]any `json:"billingAddress"`
	LifecycleStage         string         `json:"lifecycleStage"`
	OnboardedAt            *time.Time     `json:"onboardedAt,omitempty"`
	ContractStartAt        *time.Time     `json:"contractStartAt,omitempty"`
	ContractEndAt          *time.Time     `json:"contractEndAt,omitempty"`
	ChurnAt                *time.Time     `json:"churnAt,omitempty"`
	ChurnReason            string         `json:"churnReason"`
	DataSource             string         `json:"dataSource"`
	DataQualityScore       int16          `json:"dataQualityScore"`
	MergedIntoID           *int           `json:"mergedIntoId,omitempty"`
	Version                int64          `json:"version"`
	Status                 string         `json:"status"`
	LinkedCustomerTenantID *int           `json:"linkedCustomerTenantId,omitempty"`
	BranchCount            int            `json:"branchCount"`
	ContractCount          int            `json:"contractCount"`
	ContactCount           int            `json:"contactCount"`
	TenantLinkCount        int            `json:"tenantLinkCount"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
}

func mapCustomer(v *ent.ServiceCustomer) customerResponse {
	return customerResponse{
		ID:                     v.ID,
		Code:                   v.Code,
		Name:                   v.Name,
		NormalizedName:         v.NormalizedName,
		ShortName:              v.ShortName,
		LegalName:              v.LegalName,
		EnName:                 v.EnName,
		Aliases:                v.Aliases,
		HistoricalNames:        v.HistoricalNames,
		TaxID:                  v.TaxID,
		NormalizedTaxID:        v.NormalizedTaxID,
		DunsNumber:             v.DunsNumber,
		RegistrationNo:         v.RegistrationNo,
		EmailDomains:           v.EmailDomains,
		CustomerType:           v.CustomerType,
		Industry:               v.Industry,
		SubIndustry:            v.SubIndustry,
		SizeClass:              v.SizeClass,
		OwnershipType:          v.OwnershipType,
		Tier:                   v.Tier,
		VIPLevel:               v.VipLevel,
		CreditRating:           v.CreditRating,
		Website:                v.Website,
		MainPhone:              v.MainPhone,
		ServiceEmail:           v.ServiceEmail,
		Address:                v.Address,
		Timezone:               v.Timezone,
		Language:               v.Language,
		ServiceTier:            v.ServiceTier,
		SLADefinitionID:        v.SLADefinitionID,
		SupportHours:           v.SupportHours,
		CoverageScope:          v.CoverageScope,
		AccountManagerID:       v.AccountManagerID,
		ServiceOwnerID:         v.ServiceOwnerID,
		CSManagerID:            v.CsManagerID,
		OncallGroupID:          v.OncallGroupID,
		BillingCurrency:        v.BillingCurrency,
		PaymentTerms:           v.PaymentTerms,
		CreditLimit:            v.CreditLimit,
		InvoiceTitle:           v.InvoiceTitle,
		BillingAddress:         v.BillingAddress,
		LifecycleStage:         v.LifecycleStage,
		OnboardedAt:            v.OnboardedAt,
		ContractStartAt:        v.ContractStartAt,
		ContractEndAt:          v.ContractEndAt,
		ChurnAt:                v.ChurnAt,
		ChurnReason:            v.ChurnReason,
		DataSource:             v.DataSource,
		DataQualityScore:       v.DataQualityScore,
		MergedIntoID:           v.MergedIntoID,
		Version:                v.Version,
		Status:                 v.Status,
		LinkedCustomerTenantID: v.LinkedCustomerTenantID,
		CreatedAt:              v.CreatedAt,
		UpdatedAt:              v.UpdatedAt,
	}
}

type branchResponse struct {
	ID             int            `json:"id"`
	CustomerID     int            `json:"customerId"`
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	NormalizedName string         `json:"normalizedName"`
	BranchType     string         `json:"branchType"`
	Aliases        []string       `json:"aliases"`
	Address        map[string]any `json:"address"`
	Timezone       string         `json:"timezone"`
	ContactPhone   string         `json:"contactPhone"`
	EmailDomains   []string       `json:"emailDomains"`
	ServiceTier    string         `json:"serviceTier"`
	SupportHours   string         `json:"supportHours"`
	IsPrimary      bool           `json:"isPrimary"`
	LinkedTenantID *int           `json:"linkedTenantId,omitempty"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

func mapBranch(v *ent.CustomerBranch) branchResponse {
	return branchResponse{
		ID:             v.ID,
		CustomerID:     v.CustomerID,
		Code:           v.Code,
		Name:           v.Name,
		NormalizedName: v.NormalizedName,
		BranchType:     v.BranchType,
		Aliases:        v.Aliases,
		Address:        v.Address,
		Timezone:       v.Timezone,
		ContactPhone:   v.ContactPhone,
		EmailDomains:   v.EmailDomains,
		ServiceTier:    v.ServiceTier,
		SupportHours:   v.SupportHours,
		IsPrimary:      v.IsPrimary,
		LinkedTenantID: v.LinkedTenantID,
		Status:         v.Status,
		CreatedAt:      v.CreatedAt,
		UpdatedAt:      v.UpdatedAt,
	}
}

type customerContactResponse struct {
	ID              int       `json:"id"`
	CustomerID      int       `json:"customerId"`
	BranchID        *int      `json:"branchId,omitempty"`
	Name            string    `json:"name"`
	NameEn          string    `json:"nameEn"`
	Title           string    `json:"title"`
	Department      string    `json:"department"`
	Email           string    `json:"email"`
	NormalizedEmail string    `json:"normalizedEmail"`
	Mobile          string    `json:"mobile"`
	Phone           string    `json:"phone"`
	Wechat          string    `json:"wechat"`
	ContactRole     string    `json:"contactRole"`
	IsPrimary       bool      `json:"isPrimary"`
	Language        string    `json:"language"`
	Timezone        string    `json:"timezone"`
	OptIn           bool      `json:"optIn"`
	LinkedUserID    *int      `json:"linkedUserId,omitempty"`
	Source          string    `json:"source"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func mapCustomerContact(v *ent.CustomerContact) customerContactResponse {
	return customerContactResponse{
		ID:              v.ID,
		CustomerID:      v.CustomerID,
		BranchID:        v.BranchID,
		Name:            v.Name,
		NameEn:          v.NameEn,
		Title:           v.Title,
		Department:      v.Department,
		Email:           v.Email,
		NormalizedEmail: v.NormalizedEmail,
		Mobile:          v.Mobile,
		Phone:           v.Phone,
		Wechat:          v.Wechat,
		ContactRole:     v.ContactRole,
		IsPrimary:       v.IsPrimary,
		Language:        v.Language,
		Timezone:        v.Timezone,
		OptIn:           v.OptIn,
		LinkedUserID:    v.LinkedUserID,
		Source:          v.Source,
		Status:          v.Status,
		CreatedAt:       v.CreatedAt,
		UpdatedAt:       v.UpdatedAt,
	}
}

type customerTenantLinkResponse struct {
	ID              int        `json:"id"`
	CustomerID      int        `json:"customerId"`
	LinkedTenantID  int        `json:"linkedTenantId"`
	LinkType        string     `json:"linkType"`
	IsBillingAnchor bool       `json:"isBillingAnchor"`
	ProvisionStatus string     `json:"provisionStatus"`
	ProvisionedAt   *time.Time `json:"provisionedAt,omitempty"`
	Note            string     `json:"note"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

func mapCustomerTenantLink(v *ent.CustomerTenantLink) customerTenantLinkResponse {
	return customerTenantLinkResponse{
		ID:              v.ID,
		CustomerID:      v.CustomerID,
		LinkedTenantID:  v.LinkedTenantID,
		LinkType:        v.LinkType,
		IsBillingAnchor: v.IsBillingAnchor,
		ProvisionStatus: v.ProvisionStatus,
		ProvisionedAt:   v.ProvisionedAt,
		Note:            v.Note,
		CreatedAt:       v.CreatedAt,
		UpdatedAt:       v.UpdatedAt,
	}
}

func mapBranches(items []*ent.CustomerBranch) []branchResponse {
	result := make([]branchResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapBranch(item))
	}
	return result
}

func mapCustomerContacts(items []*ent.CustomerContact) []customerContactResponse {
	result := make([]customerContactResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapCustomerContact(item))
	}
	return result
}

func mapCustomerTenantLinks(items []*ent.CustomerTenantLink) []customerTenantLinkResponse {
	result := make([]customerTenantLinkResponse, 0, len(items))
	for _, item := range items {
		result = append(result, mapCustomerTenantLink(item))
	}
	return result
}

type sourceOrganizationResponse struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"normalizedName"`
	EmailAddresses []string  `json:"emailAddresses"`
	EmailDomains   []string  `json:"emailDomains"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func mapSourceOrganization(v *ent.SourceOrganization) sourceOrganizationResponse {
	return sourceOrganizationResponse{ID: v.ID, Name: v.Name, NormalizedName: v.NormalizedName, EmailAddresses: v.EmailAddresses, EmailDomains: v.EmailDomains, Status: v.Status, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

type supportContractResponse struct {
	ID                  int        `json:"id"`
	CustomerID          int        `json:"customerId"`
	BranchID            *int       `json:"branchId,omitempty"`
	ContractNumber      string     `json:"contractNumber"`
	ContractName        string     `json:"contractName"`
	ContractType        string     `json:"contractType"`
	Status              string     `json:"status"`
	StartAt             *time.Time `json:"startAt,omitempty"`
	EndAt               *time.Time `json:"endAt,omitempty"`
	EffectiveFrom       *time.Time `json:"effectiveFrom,omitempty"`
	EffectiveUntil      *time.Time `json:"effectiveUntil,omitempty"`
	SignedAt            *time.Time `json:"signedAt,omitempty"`
	Amount              *float64   `json:"amount,omitempty"`
	Currency            string     `json:"currency"`
	ParentContractID    *int       `json:"parentContractId,omitempty"`
	RenewalOfContractID *int       `json:"renewalOfContractId,omitempty"`
	AmendmentCount      int        `json:"amendmentCount"`
	SLADefinitionID     *int       `json:"slaDefinitionId,omitempty"`
	AutoRenew           bool       `json:"autoRenew"`
	TerminatedAt        *time.Time `json:"terminatedAt,omitempty"`
	TerminationReason   string     `json:"terminationReason"`
	Version             int64      `json:"version"`
	DisplayStatus       string     `json:"displayStatus,omitempty"`
	RemainingDays       *int       `json:"remainingDays,omitempty"`
	EffectiveAmount     *float64   `json:"effectiveAmount,omitempty"`
	AllocatedAmount     float64    `json:"allocatedAmount"`
	UsedAmount          float64    `json:"usedAmount"`
	RemainingAmount     *float64   `json:"remainingAmount,omitempty"`
	AvailableAmount     *float64   `json:"availableAmount,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

func mapSupportContract(v *ent.SupportContract) supportContractResponse {
	return supportContractResponse{
		ID: v.ID, CustomerID: v.CustomerID, BranchID: v.BranchID, ContractNumber: v.ContractNumber,
		ContractName: v.ContractName, ContractType: v.ContractType, Status: v.Status,
		StartAt: v.StartAt, EndAt: v.EndAt, EffectiveFrom: v.EffectiveFrom, EffectiveUntil: v.EffectiveUntil,
		SignedAt: v.SignedAt, Amount: v.Amount, Currency: v.Currency,
		ParentContractID: v.ParentContractID, RenewalOfContractID: v.RenewalOfContractID,
		AmendmentCount: v.AmendmentCount, SLADefinitionID: v.SLADefinitionID, AutoRenew: v.AutoRenew,
		TerminatedAt: v.TerminatedAt, TerminationReason: v.TerminationReason, Version: v.Version,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

type externalReferenceResponse struct {
	ID                     int       `json:"id"`
	SourceOrganizationID   int       `json:"sourceOrganizationId"`
	SupportContractID      int       `json:"supportContractId"`
	CustomerID             int       `json:"customerId"`
	BranchID               *int      `json:"branchId,omitempty"`
	ExternalContractNumber string    `json:"externalContractNumber"`
	CreatedAt              time.Time `json:"createdAt"`
}

func mapExternalReference(v *ent.ExternalContractReference) externalReferenceResponse {
	return externalReferenceResponse{ID: v.ID, SourceOrganizationID: v.SourceOrganizationID, SupportContractID: v.SupportContractID, CustomerID: v.CustomerID, BranchID: v.BranchID, ExternalContractNumber: v.ExternalContractNumber, CreatedAt: v.CreatedAt}
}

type scheduleResponse struct {
	ID        int       `json:"id"`
	GroupID   int       `json:"groupId"`
	Name      string    `json:"name"`
	Timezone  string    `json:"timezone"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func mapSchedule(v *ent.OnCallSchedule) scheduleResponse {
	return scheduleResponse{ID: v.ID, GroupID: v.GroupID, Name: v.Name, Timezone: v.Timezone, Status: v.Status, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}

type shiftResponse struct {
	ID         int       `json:"id"`
	ScheduleID int       `json:"scheduleId"`
	UserID     int       `json:"userId"`
	StartAt    time.Time `json:"startAt"`
	EndAt      time.Time `json:"endAt"`
}

type conversationResponse struct {
	ID                int       `json:"id"`
	Token             string    `json:"conversationToken"`
	Status            string    `json:"status"`
	CustomerID        *int      `json:"customerId,omitempty"`
	CustomerName      string    `json:"customerName,omitempty"`
	BranchID          *int      `json:"branchId,omitempty"`
	BranchName        string    `json:"branchName,omitempty"`
	SupportContractID *int      `json:"supportContractId,omitempty"`
	ContractNumber    string    `json:"contractNumber,omitempty"`
	IncidentID        *int      `json:"incidentId,omitempty"`
	IncidentNumber    string    `json:"incidentNumber,omitempty"`
	Confidence        float64   `json:"confidence"`
	MissingFields     []string  `json:"missingFields"`
	Version           int       `json:"version"`
	LastMessageAt     time.Time `json:"lastMessageAt"`
	CreatedAt         time.Time `json:"createdAt"`
}

func mapConversation(v *ent.EmailConversation) conversationResponse {
	result := conversationResponse{ID: v.ID, Token: v.ConversationToken, Status: v.Status, CustomerID: v.CustomerID, BranchID: v.BranchID, SupportContractID: v.SupportContractID, Confidence: v.Confidence, MissingFields: v.MissingFields, Version: v.Version, LastMessageAt: v.LastMessageAt, CreatedAt: v.CreatedAt}
	if v.Edges.Customer != nil {
		result.CustomerName = v.Edges.Customer.Name
	}
	if v.Edges.Branch != nil {
		result.BranchName = v.Edges.Branch.Name
	}
	if v.Edges.SupportContract != nil {
		result.ContractNumber = v.Edges.SupportContract.ContractNumber
	}
	if len(v.Edges.Incidents) > 0 {
		result.IncidentID = &v.Edges.Incidents[0].ID
		result.IncidentNumber = v.Edges.Incidents[0].IncidentNumber
	}
	return result
}

type inboundMessageResponse struct {
	ID                int       `json:"id"`
	ExternalMessageID string    `json:"externalMessageId"`
	FromAddress       string    `json:"fromAddress"`
	ToAddresses       []string  `json:"toAddresses"`
	Subject           string    `json:"subject"`
	PlainText         string    `json:"plainText"`
	SanitizedHTML     string    `json:"sanitizedHtml"`
	ProcessingStatus  string    `json:"processingStatus"`
	LastError         string    `json:"lastError,omitempty"`
	ReceivedAt        time.Time `json:"receivedAt"`
}
type analysisResponse struct {
	ID              int                    `json:"id"`
	Provider        string                 `json:"provider"`
	Model           string                 `json:"model"`
	PromptVersion   string                 `json:"promptVersion"`
	Result          map[string]interface{} `json:"result"`
	Confidence      float64                `json:"confidence"`
	Status          string                 `json:"status"`
	ValidationError string                 `json:"validationError,omitempty"`
	Corrections     map[string]interface{} `json:"corrections"`
	CreatedAt       time.Time              `json:"createdAt"`
}
type outboundResponse struct {
	ID        int        `json:"id"`
	ReplyType string     `json:"replyType"`
	Revision  int        `json:"revision"`
	ToAddress string     `json:"toAddress"`
	Subject   string     `json:"subject"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"lastError,omitempty"`
	SentAt    *time.Time `json:"sentAt,omitempty"`
}
type conversationDetailResponse struct {
	conversationResponse
	CanonicalData    map[string]interface{}   `json:"canonicalData"`
	FieldSources     map[string]interface{}   `json:"fieldSources"`
	Messages         []inboundMessageResponse `json:"messages"`
	Analyses         []analysisResponse       `json:"analyses"`
	OutboundMessages []outboundResponse       `json:"outboundMessages"`
}

func mapConversationDetail(v *ent.EmailConversation) conversationDetailResponse {
	result := conversationDetailResponse{conversationResponse: mapConversation(v), CanonicalData: v.CanonicalData, FieldSources: v.FieldSources}
	for _, message := range v.Edges.Messages {
		result.Messages = append(result.Messages, inboundMessageResponse{ID: message.ID, ExternalMessageID: message.ExternalMessageID, FromAddress: message.FromAddress, ToAddresses: message.ToAddresses, Subject: message.Subject, PlainText: message.PlainText, SanitizedHTML: message.SanitizedHTML, ProcessingStatus: message.ProcessingStatus, LastError: message.LastError, ReceivedAt: message.ReceivedAt})
	}
	for _, analysis := range v.Edges.Analyses {
		result.Analyses = append(result.Analyses, analysisResponse{ID: analysis.ID, Provider: analysis.Provider, Model: analysis.Model, PromptVersion: analysis.PromptVersion, Result: analysis.Result, Confidence: analysis.Confidence, Status: analysis.Status, ValidationError: analysis.ValidationError, Corrections: analysis.Corrections, CreatedAt: analysis.CreatedAt})
	}
	for _, outbound := range v.Edges.OutboundMessages {
		result.OutboundMessages = append(result.OutboundMessages, outboundResponse{ID: outbound.ID, ReplyType: outbound.ReplyType, Revision: outbound.Revision, ToAddress: outbound.ToAddress, Subject: outbound.Subject, Status: outbound.Status, Attempts: outbound.Attempts, LastError: outbound.LastError, SentAt: outbound.SentAt})
	}
	return result
}
