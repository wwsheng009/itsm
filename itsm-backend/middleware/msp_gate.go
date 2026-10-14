package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"itsm-backend/pkg/tenantmode"
)

// mspEnabled gates the entire /api/v1/msp/* route family. The bootstrap layer
// sets it from cfg.Deployment.Mode (single source: config ← DEPLOYMENT_MODE,
// default `private`) before the router serves traffic, so non-MSP deployments
// return 404 even if a route is registered.
var (
	mspEnabled     = true
	deploymentMode string
)

// SetMSPEnabled toggles the gate. main.go wires this from the deployment mode
// env var before NewApplication runs.
func SetMSPEnabled(b bool) { mspEnabled = b }

// IsMSPEnabled reports the current gate state. Useful for tests and for the
// health endpoint to surface a warning when a non-MSP deployment accidentally
// exposes the routes.
func IsMSPEnabled() bool { return mspEnabled }

// RequireMSPDeploymentMode 非 MSP 部署（IsMSPEnabled=false）时按 404 屏蔽路由。
// 用于挂载在租户面、但语义上属于 MSP 能力的端点（如 SLA 下发授权，方案 §4.1）：
// private/saas 部署下 404（canon §7 目标口径 R12/I12 收口）。
func RequireMSPDeploymentMode() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsMSPEnabled() {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    http.StatusNotFound,
				"message": "MSP routes are disabled",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// DeploymentMode reports the mode applied by ApplyDeploymentMode ("" before the
// first successful call). Used by startup logs and self-checks.
func DeploymentMode() string { return deploymentMode }

// ApplyDeploymentMode wires mspEnabled from the resolved deployment mode.
// Policy (IP-P0-1 / canon I12; previously `saas`/empty/unknown silently opened
// the MSP family — see canon R12):
//
//   - "saas_msp": MSP routes exposed (mspEnabled = true)
//   - "private" / "saas": MSP routes closed (mspEnabled = false)
//   - anything else (including empty): returns an error; the caller must treat
//     it as fatal instead of guessing. Rejected input never mutates the gate.
//
// Keeping this in middleware (not main.go) keeps the policy unit-testable
// without booting the binary.
func ApplyDeploymentMode(mode string) error {
	if err := tenantmode.ValidateDeploymentMode(mode); err != nil {
		return err
	}
	deploymentMode = mode
	SetMSPEnabled(tenantmode.MSPRoutesEnabled(mode))

	if mode == tenantmode.DeploymentModeSaaS {
		// 过渡提示（一个版本）：saas 目标态不再暴露 MSP 路由；确有 MSP 业务请显式设 saas_msp。
		if globalLogger != nil {
			globalLogger.Warnw(
				"deployment mode saas no longer exposes MSP routes; set DEPLOYMENT_MODE=saas_msp if this deployment serves MSP customers",
				"deployment_mode", mode,
			)
		}
	}
	return nil
}
