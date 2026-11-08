package email_intake

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/ent/asset"
	"itsm-backend/ent/configurationitem"
	"itsm-backend/ent/emailconversation"
	"itsm-backend/ent/incident"
	"itsm-backend/ent/project"
	"itsm-backend/ent/projectassetlink"
	"itsm-backend/ent/projectcustomerlink"
	"itsm-backend/ent/projectmilestone"
	"itsm-backend/ent/projectrisk"
	"itsm-backend/ent/slametric"
	"itsm-backend/ent/slaviolation"
	"itsm-backend/ent/supportcontract"
	tenantpkg "itsm-backend/ent/tenant"
	"itsm-backend/ent/ticket"
)

const customer360WindowDays = 30

type customer360Overview struct {
	CustomerID         int    `json:"customerId"`
	Code               string `json:"code"`
	Name               string `json:"name"`
	Tier               string `json:"tier"`
	LifecycleStage     string `json:"lifecycleStage"`
	Status             string `json:"status"`
	DataQualityScore   int16  `json:"dataQualityScore"`
	AccountManagerID   *int   `json:"accountManagerId,omitempty"`
	AccountManagerName string `json:"accountManagerName"`
	Industry           string `json:"industry"`
	CustomerType       string `json:"customerType"`
	BranchCount        int    `json:"branchCount"`
	ContactCount       int    `json:"contactCount"`
	TenantLinkCount    int    `json:"tenantLinkCount"`
	ContractCount      int    `json:"contractCount"`
}

type customer360SLAMetric struct {
	MetricType string    `json:"metricType"`
	MetricName string    `json:"metricName"`
	Value      float64   `json:"value"`
	Unit       string    `json:"unit"`
	MeasuredAt time.Time `json:"measuredAt"`
}

type customer360ServiceHealth struct {
	ComplianceRate *float64               `json:"complianceRate,omitempty"`
	Violations30d  int                    `json:"violations30d"`
	OpenViolations int                    `json:"openViolations"`
	Metrics        []customer360SLAMetric `json:"metrics"`
}

type customer360WorkItem struct {
	Kind      string    `json:"kind"`
	ID        int       `json:"id"`
	Number    string    `json:"number"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	CreatedAt time.Time `json:"createdAt"`
}

type customer360TicketSection struct {
	TicketTotal          int                   `json:"ticketTotal"`
	TicketOpen           int                   `json:"ticketOpen"`
	TicketOverdue        int                   `json:"ticketOverdue"`
	IncidentTotal        int                   `json:"incidentTotal"`
	IncidentOpen         int                   `json:"incidentOpen"`
	IncidentOverdue      int                   `json:"incidentOverdue"`
	P1P2                 int                   `json:"p1p2"`
	PriorityDistribution map[string]int        `json:"priorityDistribution"`
	Recent               []customer360WorkItem `json:"recent"`
}

type customer360Contract struct {
	ID             int        `json:"id"`
	ContractNumber string     `json:"contractNumber"`
	Status         string     `json:"status"`
	StartAt        *time.Time `json:"startAt,omitempty"`
	EndAt          *time.Time `json:"endAt,omitempty"`
	Amount         *float64   `json:"amount,omitempty"`
	RemainingDays  *int       `json:"remainingDays,omitempty"`
}

type customer360ContractSection struct {
	ActiveCount  int                   `json:"activeCount"`
	ExpiringSoon int                   `json:"expiringSoon"`
	RenewalAlert bool                  `json:"renewalAlert"`
	Current      *customer360Contract  `json:"current,omitempty"`
	Items        []customer360Contract `json:"items"`
}

type customer360Project struct {
	ID             int    `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Status         string `json:"status"`
	Health         string `json:"health"`
	Progress       int    `json:"progress"`
	OpenRisks      int    `json:"openRisks"`
	MilestoneRisks int    `json:"milestoneRisks"`
}

type customer360ProjectSection struct {
	Total          int                  `json:"total"`
	InFlight       int                  `json:"inFlight"`
	Red            int                  `json:"red"`
	Yellow         int                  `json:"yellow"`
	OpenRisks      int                  `json:"openRisks"`
	MilestoneRisks int                  `json:"milestoneRisks"`
	Items          []customer360Project `json:"items"`
}

type customer360AssetSection struct {
	AssetCount         int            `json:"assetCount"`
	CICount            int            `json:"ciCount"`
	StatusDistribution map[string]int `json:"statusDistribution"`
	KeyAssets          []string       `json:"keyAssets"`
	KeyCIs             []string       `json:"keyCIs"`
}

type customer360Contact struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Email       string `json:"email"`
	Mobile      string `json:"mobile"`
	Phone       string `json:"phone"`
	ContactRole string `json:"contactRole"`
}

type customer360ContactSection struct {
	Total      int                  `json:"total"`
	Primary    *customer360Contact  `json:"primary,omitempty"`
	Escalation *customer360Contact  `json:"escalation,omitempty"`
	Items      []customer360Contact `json:"items"`
}

type customer360Relation struct {
	Customer     relationCustomerSummary `json:"customer"`
	Role         string                  `json:"role"`
	RelationType string                  `json:"relationType"`
	Direction    string                  `json:"direction"`
}

type customer360RelationshipSection struct {
	Total    int                   `json:"total"`
	Parents  int                   `json:"parents"`
	Children int                   `json:"children"`
	Partners int                   `json:"partners"`
	Others   int                   `json:"others"`
	Items    []customer360Relation `json:"items"`
}

type customer360Tenant struct {
	LinkID          int    `json:"linkId"`
	LinkedTenantID  int    `json:"linkedTenantId"`
	TenantName      string `json:"tenantName"`
	TenantCode      string `json:"tenantCode"`
	LinkType        string `json:"linkType"`
	IsBillingAnchor bool   `json:"isBillingAnchor"`
	ProvisionStatus string `json:"provisionStatus"`
}

type customer360TenantSection struct {
	Total        int                 `json:"total"`
	Primary      int                 `json:"primary"`
	Sandbox      int                 `json:"sandbox"`
	Archive      int                 `json:"archive"`
	Provisioning int                 `json:"provisioning"`
	Items        []customer360Tenant `json:"items"`
}

type customer360ExternalID struct {
	ID          int    `json:"id"`
	SystemCode  string `json:"systemCode"`
	ExternalID  string `json:"externalId"`
	ExternalURL string `json:"externalUrl"`
}

type Customer360 struct {
	GeneratedAt   time.Time                      `json:"generatedAt"`
	WindowDays    int                            `json:"windowDays"`
	Overview      customer360Overview            `json:"overview"`
	ServiceHealth customer360ServiceHealth       `json:"serviceHealth"`
	Tickets       customer360TicketSection       `json:"tickets"`
	Contracts     customer360ContractSection     `json:"contracts"`
	Projects      customer360ProjectSection      `json:"projects"`
	Assets        customer360AssetSection        `json:"assets"`
	Contacts      customer360ContactSection      `json:"contacts"`
	Relationships customer360RelationshipSection `json:"relationships"`
	Tenants       customer360TenantSection       `json:"tenants"`
	ExternalIDs   []customer360ExternalID        `json:"externalIds"`
}

func (s *Service) GetCustomer360(ctx context.Context, tenantID, customerID int) (*Customer360, error) {
	detail, err := s.GetCustomerDetail(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	since := now.Add(-customer360WindowDays * 24 * time.Hour)
	customer := detail.Customer
	response := &Customer360{
		GeneratedAt: now,
		WindowDays:  customer360WindowDays,
		Overview: customer360Overview{
			CustomerID:         customer.ID,
			Code:               customer.Code,
			Name:               customer.Name,
			Tier:               customer.Tier,
			LifecycleStage:     customer.LifecycleStage,
			Status:             customer.Status,
			DataQualityScore:   customer.DataQualityScore,
			AccountManagerID:   customer.AccountManagerID,
			AccountManagerName: detail.AccountManagerName,
			Industry:           customer.Industry,
			CustomerType:       customer.CustomerType,
			BranchCount:        detail.BranchCount,
			ContactCount:       detail.ContactCount,
			TenantLinkCount:    detail.TenantLinkCount,
			ContractCount:      detail.ContractCount,
		},
		ExternalIDs: make([]customer360ExternalID, 0, len(detail.ExternalIDs)),
	}
	for _, externalID := range detail.ExternalIDs {
		response.ExternalIDs = append(response.ExternalIDs, customer360ExternalID{
			ID: externalID.ID, SystemCode: externalID.SystemCode, ExternalID: externalID.ExternalID, ExternalURL: externalID.ExternalURL,
		})
	}

	projectIDs, projectSection, err := s.buildCustomer360Projects(ctx, tenantID, customerID, now)
	if err != nil {
		return nil, err
	}
	response.Projects = projectSection

	ticketSection, tickets, incidents, err := s.buildCustomer360Tickets(ctx, tenantID, customerID, projectIDs, now)
	if err != nil {
		return nil, err
	}
	response.Tickets = ticketSection

	contractSection, err := s.buildCustomer360Contracts(ctx, tenantID, customerID, now)
	if err != nil {
		return nil, err
	}
	response.Contracts = contractSection

	response.ServiceHealth, err = s.buildCustomer360ServiceHealth(ctx, tenantID, customer, since, now, tickets, incidents)
	if err != nil {
		return nil, err
	}
	response.Assets, err = s.buildCustomer360Assets(ctx, tenantID, projectIDs)
	if err != nil {
		return nil, err
	}
	response.Contacts = buildCustomer360Contacts(detail.Contacts)
	response.Relationships, err = s.buildCustomer360Relationships(ctx, tenantID, customerID)
	if err != nil {
		return nil, err
	}
	response.Tenants, err = s.buildCustomer360Tenants(ctx, tenantID, detail.TenantLinks)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (s *Service) buildCustomer360Contracts(ctx context.Context, tenantID, customerID int, now time.Time) (customer360ContractSection, error) {
	contracts, err := s.client.SupportContract.Query().
		Where(supportcontract.TenantIDEQ(tenantID), supportcontract.CustomerIDEQ(customerID)).
		Order(ent.Asc(supportcontract.FieldEndAt)).
		All(ctx)
	if err != nil {
		return customer360ContractSection{}, err
	}
	section := customer360ContractSection{Items: make([]customer360Contract, 0, len(contracts))}
	for _, contract := range contracts {
		if contract.Status == "active" {
			section.ActiveCount++
		}
		value := buildCustomer360Contract(contract, now)
		if contract.Status == "active" {
			until := contract.EffectiveUntil
			if until == nil {
				until = contract.EndAt
			}
			if until != nil && until.After(now) && !until.After(now.Add(30*24*time.Hour)) {
				section.ExpiringSoon++
			}
		}
		if contract.Status == "active" && customer360ContractCurrent(contract, now) {
			if section.Current == nil || (value.EndAt != nil && (section.Current.EndAt == nil || value.EndAt.After(*section.Current.EndAt))) {
				section.Current = value
			}
		}
		if len(section.Items) < 5 {
			section.Items = append(section.Items, *value)
		}
	}
	section.RenewalAlert = section.ExpiringSoon > 0
	return section, nil
}

func customer360ContractCurrent(contract *ent.SupportContract, now time.Time) bool {
	if contract.Status != "active" {
		return false
	}
	from := contract.EffectiveFrom
	if from == nil {
		from = contract.StartAt
	}
	until := contract.EffectiveUntil
	if until == nil {
		until = contract.EndAt
	}
	if from != nil && from.After(now) {
		return false
	}
	if until != nil && !until.After(now) {
		return false
	}
	return true
}

func buildCustomer360Contract(contract *ent.SupportContract, now time.Time) *customer360Contract {
	start := contract.EffectiveFrom
	if start == nil {
		start = contract.StartAt
	}
	end := contract.EffectiveUntil
	if end == nil {
		end = contract.EndAt
	}
	item := &customer360Contract{
		ID: contract.ID, ContractNumber: contract.ContractNumber, Status: contract.Status,
		StartAt: start, EndAt: end, Amount: contract.Amount,
	}
	if item.EndAt != nil {
		days := int(item.EndAt.Sub(now).Hours() / 24)
		item.RemainingDays = &days
	}
	return item
}

func (s *Service) buildCustomer360Projects(ctx context.Context, tenantID, customerID int, now time.Time) ([]int, customer360ProjectSection, error) {
	links, err := s.client.ProjectCustomerLink.Query().
		Where(
			projectcustomerlink.TenantIDEQ(tenantID),
			projectcustomerlink.CustomerIDEQ(customerID),
			projectcustomerlink.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return nil, customer360ProjectSection{}, err
	}
	projectIDs := uniqueProjectIDs(links)
	section := customer360ProjectSection{
		Total: len(projectIDs),
		Items: make([]customer360Project, 0),
	}
	if len(projectIDs) == 0 {
		return projectIDs, section, nil
	}
	projects, err := s.client.Project.Query().
		Where(project.TenantIDEQ(tenantID), project.IDIn(projectIDs...), project.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, section, err
	}
	risks, err := s.client.ProjectRisk.Query().
		Where(projectrisk.TenantIDEQ(tenantID), projectrisk.ProjectIDIn(projectIDs...), projectrisk.DeletedAtIsNil()).
		All(ctx)
	if err != nil {
		return nil, section, err
	}
	milestones, err := s.client.ProjectMilestone.Query().
		Where(projectmilestone.TenantIDEQ(tenantID), projectmilestone.ProjectIDIn(projectIDs...)).
		All(ctx)
	if err != nil {
		return nil, section, err
	}
	openRiskByProject := map[int]int{}
	for _, risk := range risks {
		if risk.Status != "closed" {
			openRiskByProject[risk.ProjectID]++
			section.OpenRisks++
		}
	}
	milestoneRiskByProject := map[int]int{}
	for _, milestone := range milestones {
		if customer360MilestoneAtRisk(milestone, now) {
			milestoneRiskByProject[milestone.ProjectID]++
			section.MilestoneRisks++
		}
	}
	items := make([]customer360Project, 0, len(projects))
	for _, item := range projects {
		if item.Status == "completed" || item.Status == "cancelled" {
			continue
		}
		section.InFlight++
		if item.Health == "red" {
			section.Red++
		}
		if item.Health == "yellow" {
			section.Yellow++
		}
		items = append(items, customer360Project{
			ID: item.ID, Code: item.Code, Name: item.Name, Type: item.Type, Status: item.Status,
			Health: item.Health, Progress: item.Progress,
			OpenRisks: openRiskByProject[item.ID], MilestoneRisks: milestoneRiskByProject[item.ID],
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := projectHealthRank(items[i].Health), projectHealthRank(items[j].Health)
		if left == right {
			return items[i].Name < items[j].Name
		}
		return left < right
	})
	if len(items) > 5 {
		items = items[:5]
	}
	section.Items = items
	return projectIDs, section, nil
}

func uniqueProjectIDs(links []*ent.ProjectCustomerLink) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(links))
	for _, link := range links {
		if !seen[link.ProjectID] {
			seen[link.ProjectID] = true
			result = append(result, link.ProjectID)
		}
	}
	return result
}

func projectHealthRank(health string) int {
	switch health {
	case "red":
		return 0
	case "yellow":
		return 1
	default:
		return 2
	}
}

func customer360MilestoneAtRisk(milestone *ent.ProjectMilestone, now time.Time) bool {
	if milestone.Status == "missed" {
		return true
	}
	return milestone.Status == "planned" && !milestone.DueDate.After(now.Add(7*24*time.Hour))
}

func (s *Service) buildCustomer360Tickets(ctx context.Context, tenantID, customerID int, projectIDs []int, now time.Time) (customer360TicketSection, []*ent.Ticket, []*ent.Incident, error) {
	section := customer360TicketSection{
		PriorityDistribution: map[string]int{},
		Recent:               []customer360WorkItem{},
	}
	tickets := []*ent.Ticket{}
	if len(projectIDs) > 0 {
		var err error
		tickets, err = s.client.Ticket.Query().
			Where(ticket.TenantIDEQ(tenantID), ticket.ProjectIDIn(projectIDs...)).
			All(ctx)
		if err != nil {
			return section, nil, nil, err
		}
	}
	conversationIDs, err := s.client.EmailConversation.Query().
		Where(emailconversation.TenantIDEQ(tenantID), emailconversation.CustomerIDEQ(customerID)).
		Select(emailconversation.FieldID).
		Ints(ctx)
	if err != nil {
		return section, nil, nil, err
	}
	incidents := []*ent.Incident{}
	if len(projectIDs) > 0 || len(conversationIDs) > 0 {
		query := s.client.Incident.Query().Where(incident.TenantIDEQ(tenantID))
		switch {
		case len(projectIDs) > 0 && len(conversationIDs) > 0:
			query.Where(incident.Or(incident.ProjectIDIn(projectIDs...), incident.EmailConversationIDIn(conversationIDs...)))
		case len(projectIDs) > 0:
			query.Where(incident.ProjectIDIn(projectIDs...))
		default:
			query.Where(incident.EmailConversationIDIn(conversationIDs...))
		}
		incidents, err = query.All(ctx)
		if err != nil {
			return section, nil, nil, err
		}
	}
	workItems := make([]customer360WorkItem, 0, len(tickets)+len(incidents))
	for _, item := range tickets {
		section.TicketTotal++
		section.PriorityDistribution[item.Priority]++
		if item.Priority == "critical" || item.Priority == "high" {
			section.P1P2++
		}
		if customer360OpenStatus(item.Status) {
			section.TicketOpen++
			if customer360Overdue(item.SLAResponseDeadline, item.SLAResolutionDeadline, item.FirstResponseAt, item.ResolvedAt, item.Status, now) {
				section.TicketOverdue++
			}
		}
		workItems = append(workItems, customer360WorkItem{
			Kind: "ticket", ID: item.ID, Number: item.TicketNumber, Title: item.Title,
			Status: item.Status, Priority: item.Priority, CreatedAt: item.CreatedAt,
		})
	}
	for _, item := range incidents {
		section.IncidentTotal++
		section.PriorityDistribution[item.Priority]++
		if item.Priority == "critical" || item.Priority == "high" {
			section.P1P2++
		}
		if customer360OpenStatus(item.Status) {
			section.IncidentOpen++
			if customer360Overdue(item.SLAResponseDeadline, item.SLAResolutionDeadline, item.SLAFirstResponseAt, item.SLAResolvedAt, item.Status, now) {
				section.IncidentOverdue++
			}
		}
		workItems = append(workItems, customer360WorkItem{
			Kind: "incident", ID: item.ID, Number: item.IncidentNumber, Title: item.Title,
			Status: item.Status, Priority: item.Priority, CreatedAt: item.CreatedAt,
		})
	}
	sort.Slice(workItems, func(i, j int) bool { return workItems[i].CreatedAt.After(workItems[j].CreatedAt) })
	if len(workItems) > 5 {
		workItems = workItems[:5]
	}
	section.Recent = workItems
	return section, tickets, incidents, nil
}

func customer360OpenStatus(status string) bool {
	switch strings.ToLower(status) {
	case "resolved", "closed", "cancelled", "canceled":
		return false
	default:
		return true
	}
}

func customer360Overdue(responseDeadline, resolutionDeadline, firstResponse, resolved time.Time, status string, now time.Time) bool {
	if !customer360OpenStatus(status) {
		return false
	}
	if !responseDeadline.IsZero() && firstResponse.IsZero() && responseDeadline.Before(now) {
		return true
	}
	return !resolutionDeadline.IsZero() && resolved.IsZero() && resolutionDeadline.Before(now)
}

func (s *Service) buildCustomer360ServiceHealth(ctx context.Context, tenantID int, customer *ent.ServiceCustomer, since, now time.Time, tickets []*ent.Ticket, incidents []*ent.Incident) (customer360ServiceHealth, error) {
	section := customer360ServiceHealth{Metrics: []customer360SLAMetric{}}
	if customer.SLADefinitionID != nil {
		metrics, err := s.client.SLAMetric.Query().
			Where(
				slametric.TenantIDEQ(tenantID),
				slametric.SLADefinitionIDEQ(*customer.SLADefinitionID),
				slametric.MeasurementTimeGTE(since),
			).
			Order(ent.Desc(slametric.FieldMeasurementTime)).
			Limit(20).
			All(ctx)
		if err != nil {
			return section, err
		}
		for _, metric := range metrics {
			section.Metrics = append(section.Metrics, customer360SLAMetric{
				MetricType: metric.MetricType, MetricName: metric.MetricName,
				Value: metric.MetricValue, Unit: metric.Unit, MeasuredAt: metric.MeasurementTime,
			})
		}
		section.ComplianceRate = complianceRateFromMetrics(metrics)
	}

	ticketIDs := make([]int, 0, len(tickets))
	for _, item := range tickets {
		ticketIDs = append(ticketIDs, item.ID)
	}
	incidentIDs := make([]int, 0, len(incidents))
	for _, item := range incidents {
		incidentIDs = append(incidentIDs, item.ID)
	}
	violations := []*ent.SLAViolation{}
	if len(ticketIDs) > 0 {
		items, err := s.client.SLAViolation.Query().
			Where(
				slaviolation.TenantIDEQ(tenantID),
				slaviolation.TicketTypeEQ("ticket"),
				slaviolation.TicketIDIn(ticketIDs...),
				slaviolation.ViolationTimeGTE(since),
			).
			All(ctx)
		if err != nil {
			return section, err
		}
		violations = append(violations, items...)
	}
	if len(incidentIDs) > 0 {
		items, err := s.client.SLAViolation.Query().
			Where(
				slaviolation.TenantIDEQ(tenantID),
				slaviolation.TicketTypeEQ("incident"),
				slaviolation.TicketIDIn(incidentIDs...),
				slaviolation.ViolationTimeGTE(since),
			).
			All(ctx)
		if err != nil {
			return section, err
		}
		violations = append(violations, items...)
	}
	section.Violations30d = len(violations)
	for _, violation := range violations {
		if !violation.IsResolved {
			section.OpenViolations++
		}
	}
	if section.ComplianceRate == nil {
		section.ComplianceRate = complianceRateFromSLA(tickets, incidents, violations, since)
	}
	return section, nil
}

func complianceRateFromMetrics(metrics []*ent.SLAMetric) *float64 {
	for _, metric := range metrics {
		metricType := strings.ToLower(metric.MetricType)
		if !strings.Contains(metricType, "compliance") && !strings.Contains(metricType, "achievement") && !strings.Contains(metricType, "达标") {
			continue
		}
		value := metric.MetricValue
		if value <= 1 {
			value *= 100
		}
		return &value
	}
	return nil
}

func complianceRateFromSLA(tickets []*ent.Ticket, incidents []*ent.Incident, violations []*ent.SLAViolation, since time.Time) *float64 {
	eligible := 0
	for _, item := range tickets {
		if item.CreatedAt.After(since) && (!item.SLAResponseDeadline.IsZero() || !item.SLAResolutionDeadline.IsZero()) {
			eligible++
		}
	}
	for _, item := range incidents {
		if item.CreatedAt.After(since) && (!item.SLAResponseDeadline.IsZero() || !item.SLAResolutionDeadline.IsZero()) {
			eligible++
		}
	}
	if eligible == 0 {
		return nil
	}
	violating := map[string]bool{}
	for _, violation := range violations {
		violating[violation.TicketType+":"+strconv.Itoa(violation.TicketID)] = true
	}
	value := 100 * (1 - float64(len(violating))/float64(eligible))
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return &value
}

func (s *Service) buildCustomer360Assets(ctx context.Context, tenantID int, projectIDs []int) (customer360AssetSection, error) {
	section := customer360AssetSection{StatusDistribution: map[string]int{}, KeyAssets: []string{}, KeyCIs: []string{}}
	if len(projectIDs) == 0 {
		return section, nil
	}
	links, err := s.client.ProjectAssetLink.Query().
		Where(
			projectassetlink.TenantIDEQ(tenantID),
			projectassetlink.ProjectIDIn(projectIDs...),
			projectassetlink.LinkTypeEQ("asset"),
			projectassetlink.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return section, err
	}
	assetIDs := make([]int, 0, len(links))
	seen := map[int]bool{}
	for _, link := range links {
		if !seen[link.TargetID] {
			seen[link.TargetID] = true
			assetIDs = append(assetIDs, link.TargetID)
		}
	}
	if len(assetIDs) == 0 {
		return section, nil
	}
	assets, err := s.client.Asset.Query().
		Where(asset.TenantIDEQ(tenantID), asset.IDIn(assetIDs...)).
		Order(ent.Asc(asset.FieldName)).
		All(ctx)
	if err != nil {
		return section, err
	}
	section.AssetCount = len(assets)
	ciIDs := make([]int, 0, len(assets))
	ciSeen := map[int]bool{}
	for _, item := range assets {
		section.StatusDistribution[item.Status]++
		if len(section.KeyAssets) < 5 {
			section.KeyAssets = append(section.KeyAssets, item.Name)
		}
		if item.CiID != 0 && !ciSeen[item.CiID] {
			ciSeen[item.CiID] = true
			ciIDs = append(ciIDs, item.CiID)
		}
	}
	if len(ciIDs) > 0 {
		cis, err := s.client.ConfigurationItem.Query().
			Where(configurationitem.TenantIDEQ(tenantID), configurationitem.IDIn(ciIDs...)).
			Order(ent.Asc(configurationitem.FieldName)).
			All(ctx)
		if err != nil {
			return section, err
		}
		section.CICount = len(cis)
		for _, item := range cis {
			if len(section.KeyCIs) >= 5 {
				break
			}
			section.KeyCIs = append(section.KeyCIs, item.Name)
		}
	}
	return section, nil
}

func buildCustomer360Contacts(contacts []*ent.CustomerContact) customer360ContactSection {
	section := customer360ContactSection{Items: []customer360Contact{}}
	active := make([]*ent.CustomerContact, 0, len(contacts))
	for _, item := range contacts {
		if item.Status == "active" && item.DeletedAt == nil {
			active = append(active, item)
		}
	}
	section.Total = len(active)
	for _, item := range active {
		value := customer360Contact{
			ID: item.ID, Name: item.Name, Title: item.Title, Email: item.Email,
			Mobile: item.Mobile, Phone: item.Phone, ContactRole: item.ContactRole,
		}
		if len(section.Items) < 5 {
			section.Items = append(section.Items, value)
		}
		if item.IsPrimary && section.Primary == nil {
			copy := value
			section.Primary = &copy
		}
		if item.ContactRole == "escalation" && section.Escalation == nil {
			copy := value
			section.Escalation = &copy
		}
	}
	return section
}

func (s *Service) buildCustomer360Relationships(ctx context.Context, tenantID, customerID int) (customer360RelationshipSection, error) {
	rows, err := s.ListCustomerRelations(ctx, tenantID, customerID)
	if err != nil {
		return customer360RelationshipSection{}, err
	}
	section := customer360RelationshipSection{Items: []customer360Relation{}}
	roles := map[int]string{}
	items := map[int]customer360Relation{}
	for _, row := range rows {
		relation := row.Relation
		var neighbor *ent.ServiceCustomer
		var role string
		if relation.FromCustomerID == customerID {
			neighbor = row.ToCustomer
			switch relation.RelationType {
			case "parent":
				role = "child"
			case "subsidiary":
				role = "parent"
			case "partner":
				role = "partner"
			default:
				role = "other"
			}
		} else {
			neighbor = row.FromCustomer
			switch relation.RelationType {
			case "parent":
				role = "parent"
			case "subsidiary":
				role = "child"
			case "partner":
				role = "partner"
			default:
				role = "other"
			}
		}
		if neighbor == nil {
			continue
		}
		if current, exists := roles[neighbor.ID]; exists && customer360RoleRank(current) <= customer360RoleRank(role) {
			continue
		}
		roles[neighbor.ID] = role
		items[neighbor.ID] = customer360Relation{
			Customer: relationSummary(neighbor), Role: role,
			RelationType: relation.RelationType, Direction: row.Direction,
		}
	}
	section.Total = len(items)
	for _, item := range items {
		switch item.Role {
		case "parent":
			section.Parents++
		case "child":
			section.Children++
		case "partner":
			section.Partners++
		default:
			section.Others++
		}
		if len(section.Items) < 5 {
			section.Items = append(section.Items, item)
		}
	}
	return section, nil
}

func customer360RoleRank(role string) int {
	switch role {
	case "parent":
		return 0
	case "child":
		return 1
	case "partner":
		return 2
	default:
		return 3
	}
}

func (s *Service) buildCustomer360Tenants(ctx context.Context, tenantID int, links []*ent.CustomerTenantLink) (customer360TenantSection, error) {
	section := customer360TenantSection{Items: []customer360Tenant{}}
	if len(links) == 0 {
		return section, nil
	}
	ids := make([]int, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.LinkedTenantID)
	}
	tenants, err := s.client.Tenant.Query().
		Where(tenantpkg.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return section, err
	}
	byID := map[int]*ent.Tenant{}
	for _, item := range tenants {
		byID[item.ID] = item
	}
	section.Total = len(links)
	for _, link := range links {
		item := customer360Tenant{
			LinkID: link.ID, LinkedTenantID: link.LinkedTenantID, LinkType: link.LinkType,
			IsBillingAnchor: link.IsBillingAnchor, ProvisionStatus: link.ProvisionStatus,
		}
		if target := byID[link.LinkedTenantID]; target != nil {
			item.TenantName = target.Name
			item.TenantCode = target.Code
		}
		switch link.LinkType {
		case "primary":
			section.Primary++
		case "sandbox":
			section.Sandbox++
		case "archive":
			section.Archive++
		}
		if link.ProvisionStatus != "ready" {
			section.Provisioning++
		}
		if len(section.Items) < 5 {
			section.Items = append(section.Items, item)
		}
	}
	return section, nil
}
