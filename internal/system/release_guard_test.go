package system

import "testing"

func TestStableSystemContractIsFrozenAtV599(t *testing.T) {
	contract := StableSystemContract()
	if contract.Version != StableReleaseVersion || contract.Status != StableReleaseStatus {
		t.Fatalf("unexpected release identity: %+v", contract)
	}
	if contract.RuntimeModel != "causal-runtime-os-v1" || contract.PredictionMode != "advisory-only-isolated" {
		t.Fatalf("unexpected runtime contract: %+v", contract)
	}
	if err := ValidateSystemContract(contract); err != nil {
		t.Fatalf("stable system contract did not validate: %v", err)
	}
	contract.PredictionMode = "execution-influencing"
	if err := ValidateSystemContract(contract); err == nil {
		t.Fatal("mutated system contract validated")
	}
}

func TestArchitectureLockBlocksRuntimeEvolution(t *testing.T) {
	for _, action := range []string{
		"control_plane_modification",
		"execution_model_change",
		"equilibrium_rewrite",
		"runtime_topology_change",
		"v6_field_model_activation",
	} {
		if err := EnforceArchitectureLock("memory-v5", action); err == nil {
			t.Fatalf("action %q was not blocked", action)
		}
	}
	if err := EnforceArchitectureLock("memory-v5", "diagnostic_report"); err != nil {
		t.Fatalf("diagnostic action was blocked: %v", err)
	}
}

func TestV6IsolationAllowsDiagnosticsOnly(t *testing.T) {
	isolation := V6Isolation{}
	for _, signal := range []string{"LayerCollapseReport", "CausalFieldSuggestion", "ArchitectureSuggestion"} {
		if !isolation.Validate(signal) {
			t.Fatalf("diagnostic signal %q was rejected", signal)
		}
	}
	for _, signal := range []string{"ExecutionOverride", "RuntimeMutation", "ControlPlaneRewrite"} {
		if isolation.Validate(signal) {
			t.Fatalf("runtime signal %q was accepted", signal)
		}
	}
}

func TestReleaseGuardRequiresAllProductionChecks(t *testing.T) {
	if err := ReleaseGuard(); err != nil {
		t.Fatalf("stable release guard failed: %v", err)
	}
	checks := StableProductionReadinessChecks()
	checks.Observability = false
	if AllChecksPassed(checks) {
		t.Fatal("incomplete production readiness checks passed")
	}
}
