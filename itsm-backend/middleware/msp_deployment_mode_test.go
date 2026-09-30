package middleware

import "testing"

// TestApplyDeploymentMode locks the IP-P0-1 policy (canon I12/R12):
// only saas_msp opens the MSP route family; private/saas close it; empty or
// unknown values are rejected so a typo can never silently open MSP.
func TestApplyDeploymentMode(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		wantMSP bool
		wantErr bool
	}{
		{"private disables MSP", "private", false, false},
		{"saas disables MSP (target I12)", "saas", false, false},
		{"saas_msp enables MSP", "saas_msp", true, false},
		{"empty is rejected", "", false, true},
		{"unknown mode is rejected", "edge", false, true},
		{"uppercase PRIVATE is rejected (case-sensitive)", "PRIVATE", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset before each case so leftover state doesn't leak.
			SetMSPEnabled(true)
			err := ApplyDeploymentMode(tc.mode)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ApplyDeploymentMode(%q) error=nil, want error", tc.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyDeploymentMode(%q) unexpected error: %v", tc.mode, err)
			}
			if got := IsMSPEnabled(); got != tc.wantMSP {
				t.Fatalf("ApplyDeploymentMode(%q) -> IsMSPEnabled()=%v, want %v",
					tc.mode, got, tc.wantMSP)
			}
		})
	}
}

// TestApplyDeploymentMode_RejectedInputDoesNotMutate ensures a rejected mode
// leaves both the gate and the recorded mode untouched (fail-closed, no guessing).
func TestApplyDeploymentMode_RejectedInputDoesNotMutate(t *testing.T) {
	if err := ApplyDeploymentMode("saas_msp"); err != nil {
		t.Fatalf("seed gate: %v", err)
	}
	beforeMode, beforeGate := DeploymentMode(), IsMSPEnabled()

	if err := ApplyDeploymentMode("bogus"); err == nil {
		t.Fatal("ApplyDeploymentMode(bogus) error=nil, want error")
	}
	if DeploymentMode() != beforeMode || IsMSPEnabled() != beforeGate {
		t.Fatalf("rejected mode mutated state: mode %q->%q gate %v->%v",
			beforeMode, DeploymentMode(), beforeGate, IsMSPEnabled())
	}
}

// TestApplyDeploymentMode_Idempotent makes sure calling ApplyDeploymentMode
// twice with the same mode is safe and leaves the gate in the right state.
func TestApplyDeploymentMode_Idempotent(t *testing.T) {
	for _, mode := range []string{"private", "saas", "saas_msp"} {
		if err := ApplyDeploymentMode(mode); err != nil {
			t.Fatalf("ApplyDeploymentMode(%q) unexpected error: %v", mode, err)
		}
		firstGate, firstMode := IsMSPEnabled(), DeploymentMode()
		if err := ApplyDeploymentMode(mode); err != nil {
			t.Fatalf("ApplyDeploymentMode(%q) second call error: %v", mode, err)
		}
		if IsMSPEnabled() != firstGate || DeploymentMode() != firstMode {
			t.Fatalf("idempotency broken for mode=%q: gate %v->%v mode %q->%q",
				mode, firstGate, IsMSPEnabled(), firstMode, DeploymentMode())
		}
	}
}
