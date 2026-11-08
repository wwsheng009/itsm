package email_intake

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"

	"itsm-backend/ent"
	"itsm-backend/ent/customercontact"
	"itsm-backend/ent/servicecustomer"
)

// CustomerMatchCandidate is persisted in email_conversations.canonical_data
// when the matcher cannot auto-bind a customer (L5 / multiple exact hits).
type CustomerMatchCandidate struct {
	ID             int     `json:"id"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Tier           string  `json:"tier"`
	LifecycleStage string  `json:"lifecycleStage"`
	Status         string  `json:"status"`
	Level          string  `json:"level"`
	Confidence     float64 `json:"confidence"`
	Reason         string  `json:"reason"`
}

type customerMatchResult struct {
	CustomerID int
	Level      string
	Field      string
	Value      string
	Confidence float64
	Candidates []CustomerMatchCandidate
	Ambiguous  bool
	Reason     string
}

var unifiedSocialCreditCodePattern = regexp.MustCompile(`[0-9A-HJ-NPQRTUWXY]{18}`)

func (r *Resolver) matchCustomer(ctx context.Context, tenantID int, fields IntakeFields) (customerMatchResult, error) {
	customers, err := r.client.ServiceCustomer.Query().
		Where(
			servicecustomer.TenantIDEQ(tenantID),
			servicecustomer.StatusEQ("active"),
			servicecustomer.DeletedAtIsNil(),
		).
		All(ctx)
	if err != nil {
		return customerMatchResult{}, fmt.Errorf("query service customers: %w", err)
	}

	if domain := senderDomain(fields.SenderEmail); domain != "" {
		matches := make([]*ent.ServiceCustomer, 0, 1)
		for _, customer := range customers {
			for _, candidate := range customer.EmailDomains {
				if normalizeDomain(candidate) == domain {
					matches = append(matches, customer)
					break
				}
			}
		}
		if len(matches) > 0 {
			return buildCustomerMatch(matches, "L1", "senderDomain", domain, 0.95, "sender_domain"), nil
		}
	}

	if email := normalizeEmail(fields.SenderEmail); email != "" {
		contacts, err := r.client.CustomerContact.Query().
			Where(
				customercontact.TenantIDEQ(tenantID),
				customercontact.NormalizedEmailEQ(email),
				customercontact.StatusEQ("active"),
				customercontact.DeletedAtIsNil(),
			).
			All(ctx)
		if err != nil {
			return customerMatchResult{}, fmt.Errorf("query customer contacts by email: %w", err)
		}
		byID := make(map[int]*ent.ServiceCustomer, len(customers))
		for _, customer := range customers {
			byID[customer.ID] = customer
		}
		seen := map[int]bool{}
		matches := make([]*ent.ServiceCustomer, 0, len(contacts))
		for _, contact := range contacts {
			if customer := byID[contact.CustomerID]; customer != nil && !seen[customer.ID] {
				seen[customer.ID] = true
				matches = append(matches, customer)
			}
		}
		if len(matches) > 0 {
			return buildCustomerMatch(matches, "L2", "contactEmail", email, 0.95, "contact_email"), nil
		}
	}

	if taxID := taxIDFromIntakeFields(fields); taxID != "" {
		matches := make([]*ent.ServiceCustomer, 0, 1)
		for _, customer := range customers {
			if customer.NormalizedTaxID != "" && customer.NormalizedTaxID == taxID {
				matches = append(matches, customer)
			}
		}
		if len(matches) > 0 {
			return buildCustomerMatch(matches, "L3", "taxId", taxID, 0.90, "tax_id"), nil
		}
	}

	needle := NormalizeName(fields.CustomerName)
	if needle != "" {
		matches := make([]*ent.ServiceCustomer, 0, 1)
		for _, customer := range customers {
			if customerExactNameMatch(customer, needle) {
				matches = append(matches, customer)
			}
		}
		if len(matches) > 0 {
			return buildCustomerMatch(matches, "L4", "customerName", fields.CustomerName, 0.85, "name_exact"), nil
		}
	}

	if needle != "" {
		candidates := fuzzyCustomerCandidates(customers, needle)
		if len(candidates) > 0 {
			return customerMatchResult{
				Level:      "L5",
				Field:      "customerName",
				Value:      fields.CustomerName,
				Confidence: candidates[0].Confidence,
				Candidates: candidates,
				Reason:     "customer_fuzzy_candidates",
			}, nil
		}
	}

	return customerMatchResult{}, nil
}

func buildCustomerMatch(matches []*ent.ServiceCustomer, level, field, value string, confidence float64, reason string) customerMatchResult {
	result := customerMatchResult{
		Level:      level,
		Field:      field,
		Value:      value,
		Confidence: confidence,
	}
	if len(matches) == 1 {
		result.CustomerID = matches[0].ID
		return result
	}
	result.Ambiguous = true
	result.Reason = "multiple_customer_matches"
	result.Candidates = customerCandidates(matches, level, confidence, reason)
	return result
}

func customerExactNameMatch(customer *ent.ServiceCustomer, needle string) bool {
	values := []string{customer.Name, customer.ShortName, customer.LegalName, customer.EnName}
	values = append(values, customer.Aliases...)
	values = append(values, customer.HistoricalNames...)
	for _, value := range values {
		if value != "" && NormalizeName(value) == needle {
			return true
		}
	}
	return false
}

func customerCandidates(matches []*ent.ServiceCustomer, level string, confidence float64, reason string) []CustomerMatchCandidate {
	result := make([]CustomerMatchCandidate, 0, len(matches))
	for _, customer := range matches {
		result = append(result, CustomerMatchCandidate{
			ID:             customer.ID,
			Code:           customer.Code,
			Name:           customer.Name,
			Tier:           customer.Tier,
			LifecycleStage: customer.LifecycleStage,
			Status:         customer.Status,
			Level:          level,
			Confidence:     confidence,
			Reason:         reason,
		})
	}
	return result
}

func fuzzyCustomerCandidates(customers []*ent.ServiceCustomer, needle string) []CustomerMatchCandidate {
	type scored struct {
		customer   *ent.ServiceCustomer
		confidence float64
		reason     string
	}
	best := map[int]scored{}
	for _, customer := range customers {
		values := []string{customer.Name, customer.ShortName, customer.LegalName, customer.EnName}
		values = append(values, customer.Aliases...)
		values = append(values, customer.HistoricalNames...)
		for _, value := range values {
			normalized := NormalizeName(value)
			if normalized == "" || normalized == needle {
				continue
			}
			confidence := 0.0
			reason := ""
			if strings.Contains(normalized, needle) || strings.Contains(needle, normalized) {
				confidence = 0.75
				reason = "fuzzy_contains"
			}
			distance := levenshtein(normalized, needle)
			if distance > 0 && distance <= 2 {
				editConfidence := 0.82 - 0.06*float64(distance)
				if editConfidence > confidence {
					confidence = editConfidence
					reason = "fuzzy_edit_distance"
				}
			}
			if confidence < 0.60 {
				continue
			}
			if confidence > 0.80 {
				confidence = 0.80
			}
			current, exists := best[customer.ID]
			if !exists || confidence > current.confidence {
				best[customer.ID] = scored{customer: customer, confidence: confidence, reason: reason}
			}
		}
	}
	items := make([]scored, 0, len(best))
	for _, item := range best {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].confidence == items[j].confidence {
			return items[i].customer.Name < items[j].customer.Name
		}
		return items[i].confidence > items[j].confidence
	})
	result := make([]CustomerMatchCandidate, 0, len(items))
	for _, item := range items {
		result = append(result, CustomerMatchCandidate{
			ID:             item.customer.ID,
			Code:           item.customer.Code,
			Name:           item.customer.Name,
			Tier:           item.customer.Tier,
			LifecycleStage: item.customer.LifecycleStage,
			Status:         item.customer.Status,
			Level:          "L5",
			Confidence:     item.confidence,
			Reason:         item.reason,
		})
	}
	return result
}

func senderDomain(value string) string {
	address := strings.TrimSpace(value)
	if parsed, err := mail.ParseAddress(address); err == nil {
		address = parsed.Address
	}
	at := strings.LastIndex(address, "@")
	if at < 0 || at == len(address)-1 {
		return ""
	}
	return normalizeDomain(address[at+1:])
}

func normalizeDomain(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@"))), ".")
}

func taxIDFromIntakeFields(fields IntakeFields) string {
	if value := normalizeTaxID(fields.TaxID); value != "" {
		return value
	}
	text := strings.ToUpper(fields.CustomerName + "\n" + fields.Description)
	if match := unifiedSocialCreditCodePattern.FindString(text); match != "" {
		return normalizeTaxID(match)
	}
	return ""
}

func levenshtein(a, b string) int {
	left := []rune(a)
	right := []rune(b)
	if len(left) == 0 {
		return len(right)
	}
	if len(right) == 0 {
		return len(left)
	}
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for i := 1; i <= len(left); i++ {
		current[0] = i
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			current[j] = matchMin(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}

func matchMin(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}
