package email_intake

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRegisterRoutesExposesMasterDataAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(nil)
	h.RegisterRoutes(r.Group("/api/v1"))

	paths := make(map[string]bool, len(r.Routes()))
	for _, route := range r.Routes() {
		paths[route.Method+" "+route.Path] = true
	}

	expected := []string{
		"GET /api/v1/email-intake/customers",
		"GET /api/v1/email-intake/branches",
		"GET /api/v1/email-intake/source-organizations",
		"GET /api/v1/email-intake/support-contracts",
		"GET /api/v1/email-intake/external-contract-references",
		"GET /api/v1/master-data/customers",
		"POST /api/v1/master-data/customers",
		"GET /api/v1/master-data/customers/:id",
		"PUT /api/v1/master-data/customers/:id",
		"DELETE /api/v1/master-data/customers/:id",
		"POST /api/v1/master-data/customers/:id/merge",
		"POST /api/v1/master-data/customers/:id/merge/undo",
		"GET /api/v1/master-data/customers/:id/merge-history",
		"GET /api/v1/master-data/customers/:id/360",
		"GET /api/v1/master-data/customers/:id/contacts",
		"POST /api/v1/master-data/customers/:id/contacts",
		"PUT /api/v1/master-data/customers/:id/contacts/:contactId",
		"DELETE /api/v1/master-data/customers/:id/contacts/:contactId",
		"GET /api/v1/master-data/customers/:id/tenant-links",
		"POST /api/v1/master-data/customers/:id/tenant-links",
		"PUT /api/v1/master-data/customers/:id/tenant-links/:linkId",
		"DELETE /api/v1/master-data/customers/:id/tenant-links/:linkId",
		"GET /api/v1/master-data/customers/:id/external-ids",
		"POST /api/v1/master-data/customers/:id/external-ids",
		"PUT /api/v1/master-data/customers/:id/external-ids/:externalId",
		"DELETE /api/v1/master-data/customers/:id/external-ids/:externalId",
		"GET /api/v1/master-data/customers/:id/relationships",
		"POST /api/v1/master-data/customers/:id/relationships",
		"PUT /api/v1/master-data/customers/:id/relationships/:relationId",
		"DELETE /api/v1/master-data/customers/:id/relationships/:relationId",
		"GET /api/v1/master-data/customers/:id/relations-tree",
		"GET /api/v1/master-data/customers/:id/projects",
		"POST /api/v1/master-data/customers/:id/projects",
		"DELETE /api/v1/master-data/customers/:id/projects/:linkId",
		"GET /api/v1/projects/:id/customer-links",
		"POST /api/v1/projects/:id/customer-links",
		"DELETE /api/v1/projects/:id/customer-links/:linkId",
		"GET /api/v1/master-data/branches",
		"GET /api/v1/master-data/source-organizations",
		"GET /api/v1/master-data/support-contracts",
		"GET /api/v1/master-data/support-contracts/:id",
		"POST /api/v1/master-data/support-contracts/:id/sign",
		"POST /api/v1/master-data/support-contracts/:id/terminate",
		"POST /api/v1/master-data/support-contracts/:id/renew",
		"GET /api/v1/master-data/support-contracts/:id/items",
		"POST /api/v1/master-data/support-contracts/:id/items",
		"PUT /api/v1/master-data/support-contracts/:id/items/:itemId",
		"DELETE /api/v1/master-data/support-contracts/:id/items/:itemId",
		"GET /api/v1/master-data/support-contracts/:id/amendments",
		"POST /api/v1/master-data/support-contracts/:id/amendments",
		"POST /api/v1/master-data/support-contracts/:id/amendments/:amendmentId/effect",
		"GET /api/v1/master-data/support-contracts/:id/usage",
		"POST /api/v1/master-data/support-contracts/:id/usage",
		"GET /api/v1/master-data/support-contracts/:id/balance",
		"GET /api/v1/master-data/support-contracts/:id/projects",
		"POST /api/v1/master-data/support-contracts/:id/projects",
		"DELETE /api/v1/master-data/support-contracts/:id/projects/:linkId",
		"GET /api/v1/master-data/external-contract-references",
		"GET /api/v1/projects/:id/contract-links",
	}
	for _, route := range expected {
		assert.True(t, paths[route], "missing route %s", route)
	}

	assert.True(t, paths["GET /api/v1/email-intake/on-call/schedules"])
	assert.False(t, paths["GET /api/v1/master-data/on-call/schedules"], "on-call remains email-intake-specific")
}
