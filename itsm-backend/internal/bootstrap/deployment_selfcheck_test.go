package bootstrap

import (
	"testing"

	"itsm-backend/pkg/tenantmode"
)

func TestDeploymentShapeMismatch(t *testing.T) {
	cases := []struct {
		name         string
		mode         string
		providers    int
		mspEnabled   bool
		wantMismatch bool
	}{
		{"saas_msp with provider and gate on passes", tenantmode.DeploymentModeSaaSMSP, 1, true, false},
		{"saas_msp without provider warns", tenantmode.DeploymentModeSaaSMSP, 0, true, true},
		{"saas_msp with gate off warns", tenantmode.DeploymentModeSaaSMSP, 1, false, true},
		{"private with gate off passes", tenantmode.DeploymentModePrivate, 0, false, false},
		{"private with gate on warns", tenantmode.DeploymentModePrivate, 0, true, true},
		{"saas with gate off passes", tenantmode.DeploymentModeSaaS, 0, false, false},
		{"saas with provider tenant warns", tenantmode.DeploymentModeSaaS, 1, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := deploymentShapeMismatch(tc.mode, tc.providers, tc.mspEnabled)
			if tc.wantMismatch && err == nil {
				t.Fatalf("deploymentShapeMismatch(%q, %d, %v) = nil, want mismatch",
					tc.mode, tc.providers, tc.mspEnabled)
			}
			if !tc.wantMismatch && err != nil {
				t.Fatalf("deploymentShapeMismatch(%q, %d, %v) = %v, want nil",
					tc.mode, tc.providers, tc.mspEnabled, err)
			}
		})
	}
}

// TestDeploymentOrphanTenantWarning 锁定存量孤儿租户告警（写路径校验落地前的遗留数据）：
// saas_msp 下的直客、非 saas_msp 下的 msp_customer 均应被点名；合法形态不告警。
func TestDeploymentOrphanTenantWarning(t *testing.T) {
	cases := []struct {
		name          string
		mode          string
		saasCustomers int
		mspCustomers  int
		wantMismatch  bool
	}{
		{"saas_msp with direct customers warns", tenantmode.DeploymentModeSaaSMSP, 2, 3, true},
		{"saas_msp without direct customers passes", tenantmode.DeploymentModeSaaSMSP, 0, 3, false},
		{"saas with msp customers warns", tenantmode.DeploymentModeSaaS, 1, 1, true},
		{"private with msp customers warns", tenantmode.DeploymentModePrivate, 0, 1, true},
		{"saas with direct customers only passes", tenantmode.DeploymentModeSaaS, 2, 0, false},
		{"private clean passes", tenantmode.DeploymentModePrivate, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := deploymentOrphanTenantWarning(tc.mode, tc.saasCustomers, tc.mspCustomers)
			if tc.wantMismatch && err == nil {
				t.Fatalf("deploymentOrphanTenantWarning(%q, %d, %d) = nil, want warning",
					tc.mode, tc.saasCustomers, tc.mspCustomers)
			}
			if !tc.wantMismatch && err != nil {
				t.Fatalf("deploymentOrphanTenantWarning(%q, %d, %d) = %v, want nil",
					tc.mode, tc.saasCustomers, tc.mspCustomers, err)
			}
		})
	}
}
