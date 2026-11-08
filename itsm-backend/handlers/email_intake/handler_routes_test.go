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
		"GET /api/v1/master-data/branches",
		"GET /api/v1/master-data/source-organizations",
		"GET /api/v1/master-data/support-contracts",
		"GET /api/v1/master-data/external-contract-references",
	}
	for _, route := range expected {
		assert.True(t, paths[route], "missing route %s", route)
	}

	assert.True(t, paths["GET /api/v1/email-intake/on-call/schedules"])
	assert.False(t, paths["GET /api/v1/master-data/on-call/schedules"], "on-call remains email-intake-specific")
}
