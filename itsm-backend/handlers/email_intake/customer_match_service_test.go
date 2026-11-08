package email_intake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"itsm-backend/ent"
	"itsm-backend/ent/enttest"
)

func TestCustomerMatcherP2_FiveLevels(t *testing.T) {
	client := openCustomerMasterTestClient(t, "customer-matcher-p2")
	defer client.Close()
	ctx := context.Background()
	provider, _ := seedProviderAndCustomerTenant(t, client, "matcher-provider", "matcher-customer")

	domainCustomer := mustCreateP2Customer(t, client, provider.ID, "Acme Domain", nil)
	domainCustomer = client.ServiceCustomer.UpdateOneID(domainCustomer.ID).
		SetEmailDomains([]string{"acme.example"}).SaveX(ctx)
	domainContract := mustCreateP2Contract(t, client, provider.ID, domainCustomer.ID, "C-L1")

	contactCustomer := mustCreateP2Customer(t, client, provider.ID, "Contact Customer", nil)
	client.CustomerContact.Create().
		SetTenantID(provider.ID).SetCustomerID(contactCustomer.ID).
		SetName("Bob").SetEmail("bob@contact.example").SetNormalizedEmail("bob@contact.example").
		SetContactRole("technical").SetStatus("active").SaveX(ctx)
	contactContract := mustCreateP2Contract(t, client, provider.ID, contactCustomer.ID, "C-L2")

	taxCustomer := mustCreateP2Customer(t, client, provider.ID, "Tax Customer", nil)
	taxCustomer = client.ServiceCustomer.UpdateOneID(taxCustomer.ID).
		SetTaxID("913100000000000001").SetNormalizedTaxID(normalizeTaxID("913100000000000001")).SaveX(ctx)
	taxContract := mustCreateP2Contract(t, client, provider.ID, taxCustomer.ID, "C-L3")

	nameCustomer := mustCreateP2Customer(t, client, provider.ID, "Delta Industries", nil)
	nameContract := mustCreateP2Contract(t, client, provider.ID, nameCustomer.ID, "C-L4")

	fuzzyCustomer := mustCreateP2Customer(t, client, provider.ID, "Global Acme Corporation", nil)
	mustCreateP2Contract(t, client, provider.ID, fuzzyCustomer.ID, "C-L5")

	resolver := NewResolver(client)

	result, err := resolver.Resolve(ctx, provider.ID, IntakeFields{
		SenderEmail: "alice@acme.example", CustomerName: "Wrong Name", ReportedContractNumber: "C-L1",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionVerified, result.Status)
	require.Equal(t, domainCustomer.ID, result.CustomerID)
	require.Equal(t, domainContract.ID, result.SupportContractID)
	require.Equal(t, "L1", result.MatchedLevel)
	require.InDelta(t, 0.95, result.MatchConfidence, 0.001)

	result, err = resolver.Resolve(ctx, provider.ID, IntakeFields{
		SenderEmail: "bob@contact.example", CustomerName: "Wrong Name", ReportedContractNumber: "C-L2",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionVerified, result.Status)
	require.Equal(t, contactCustomer.ID, result.CustomerID)
	require.Equal(t, contactContract.ID, result.SupportContractID)
	require.Equal(t, "L2", result.MatchedLevel)

	result, err = resolver.Resolve(ctx, provider.ID, IntakeFields{
		CustomerName: "Wrong Name", Description: "统一社会信用代码：913100000000000001",
		ReportedContractNumber: "C-L3",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionVerified, result.Status)
	require.Equal(t, taxCustomer.ID, result.CustomerID)
	require.Equal(t, taxContract.ID, result.SupportContractID)
	require.Equal(t, "L3", result.MatchedLevel)

	result, err = resolver.Resolve(ctx, provider.ID, IntakeFields{
		CustomerName: "Delta Industries", ReportedContractNumber: "C-L4",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionVerified, result.Status)
	require.Equal(t, nameCustomer.ID, result.CustomerID)
	require.Equal(t, nameContract.ID, result.SupportContractID)
	require.Equal(t, "L4", result.MatchedLevel)

	result, err = resolver.Resolve(ctx, provider.ID, IntakeFields{
		CustomerName: "Global Acme Corporaton", ReportedContractNumber: "C-L5",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionManualReview, result.Status)
	require.Zero(t, result.CustomerID)
	require.Equal(t, "L5", result.MatchedLevel)
	require.Less(t, result.MatchConfidence, 0.85)
	require.NotEmpty(t, result.Candidates)
	require.Equal(t, fuzzyCustomer.ID, result.Candidates[0].ID)
}

func mustCreateP2Customer(t *testing.T, client *ent.Client, tenantID int, name string, aliases []string) *ent.ServiceCustomer {
	t.Helper()
	builder := client.ServiceCustomer.Create().
		SetTenantID(tenantID).
		SetName(name).
		SetNormalizedName(NormalizeName(name)).
		SetStatus("active").
		SetLifecycleStage("active")
	if aliases != nil {
		builder.SetAliases(aliases)
	}
	return builder.SaveX(context.Background())
}

func mustCreateP2Contract(t *testing.T, client *ent.Client, tenantID, customerID int, number string) *ent.SupportContract {
	t.Helper()
	return client.SupportContract.Create().
		SetTenantID(tenantID).
		SetCustomerID(customerID).
		SetContractNumber(number).
		SetNormalizedContractNumber(NormalizeContractNumber(number)).
		SetStatus("active").
		SaveX(context.Background())
}

func TestOrchestratorP2_PersistsDomainMatchAndFuzzyCandidates(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:customer-matcher-orchestrator-p2?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := seedProviderAndCustomerTenant(t, client, "matcher-orchestrator-provider", "matcher-orchestrator-customer")
	domainCustomer := mustCreateP2Customer(t, client, tenant.ID, "Domain Matched Customer", nil)
	domainCustomer = client.ServiceCustomer.UpdateOneID(domainCustomer.ID).
		SetEmailDomains([]string{"matched.example"}).SaveX(ctx)
	mustCreateP2Contract(t, client, tenant.ID, domainCustomer.ID, "C-MATCH")

	extractor := NewEmailIntakeExtractor(fakeEmailLLM{output: `{"intent":"report_incident","sourceOrganizationName":"","customerName":"Wrong Name","reportedContractNumber":"C-MATCH","title":"P2 匹配","description":"P2","occurredAt":"","impact":"high","urgency":"high","missingFields":[],"confidence":0.96}`}, "test-model")
	orchestrator := NewEmailIntakeOrchestrator(client, extractor, nil, OrchestratorConfig{Mode: ModeManualConfirm})
	conversation, err := orchestrator.Ingest(ctx, tenant.ID, ReceivedEmail{
		SenderAuthenticated: true, MailboxInstanceKey: "matcher-box", UIDValidity: 1, UID: 1,
		ExternalMessageID: "<matcher-p2@example.com>", FromAddress: "sender@matched.example",
		Subject: "P2 匹配", PlainText: "P2", RawMIME: []byte("matcher-p2"),
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionVerified, conversation.Status)
	require.NotNil(t, conversation.CustomerID)
	require.Equal(t, domainCustomer.ID, *conversation.CustomerID)
	require.Equal(t, "L1", conversation.CanonicalData["matchLevel"])
	require.NotNil(t, conversation.FieldSources["customerId"])

	fuzzyCustomer := mustCreateP2Customer(t, client, tenant.ID, "Global Fuzzy Corporation", nil)
	require.NotZero(t, fuzzyCustomer.ID)
	extractor = NewEmailIntakeExtractor(fakeEmailLLM{output: `{"intent":"report_incident","sourceOrganizationName":"","customerName":"Global Fuzzy Corporaton","reportedContractNumber":"C-MISSING","title":"P2 模糊","description":"P2","occurredAt":"","impact":"medium","urgency":"medium","missingFields":[],"confidence":0.96}`}, "test-model")
	orchestrator = NewEmailIntakeOrchestrator(client, extractor, nil, OrchestratorConfig{Mode: ModeManualConfirm})
	conversation, err = orchestrator.Ingest(ctx, tenant.ID, ReceivedEmail{
		SenderAuthenticated: true, MailboxInstanceKey: "matcher-box", UIDValidity: 1, UID: 2,
		ExternalMessageID: "<matcher-p2-fuzzy@example.com>", FromAddress: "sender@unknown.example",
		Subject: "P2 模糊", PlainText: "P2 模糊", RawMIME: []byte("matcher-p2-fuzzy"),
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionManualReview, conversation.Status)
	require.Nil(t, conversation.CustomerID)
	require.Equal(t, "L5", conversation.CanonicalData["matchLevel"])
	require.NotNil(t, conversation.CanonicalData["candidates"])
}
