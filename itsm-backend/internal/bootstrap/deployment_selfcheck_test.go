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
