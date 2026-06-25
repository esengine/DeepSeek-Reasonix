package system

const (
	StableReleaseVersion = "v5.9.9-stable"
	StableReleaseStatus  = "RELEASE_CANDIDATE"
)

// SystemContract defines the frozen architecture boundary of the Reasonix v5
// stable release candidate. It is intentionally value-returned by
// StableSystemContract instead of being exposed as mutable package state.
type SystemContract struct {
	Version           string
	Status            string
	RuntimeModel      string
	ExecutionMode     string
	ControlPlane      string
	EquilibriumLayer  string
	MemorySystem      string
	CompressionSystem string
	SandboxModel      string
	BudgetModel       string
	SnapshotModel     string
	PredictionMode    string
}

func StableSystemContract() SystemContract {
	return SystemContract{
		Version:           StableReleaseVersion,
		Status:            StableReleaseStatus,
		RuntimeModel:      "causal-runtime-os-v1",
		ExecutionMode:     "deterministic-event-driven",
		ControlPlane:      "distributed-control-plane-v1",
		EquilibriumLayer:  "global-equilibrium-v1",
		MemorySystem:      "causal-memory-compiler-v1",
		CompressionSystem: "causal-compression-v1",
		SandboxModel:      "goroutine-contained-runtime",
		BudgetModel:       "ledger-based-2-phase-commit",
		SnapshotModel:     "atomic-barrier-snapshot",
		PredictionMode:    "advisory-only-isolated",
	}
}

func ValidateSystemContract(contract SystemContract) error {
	expected := StableSystemContract()
	if contract != expected {
		return errContractMismatch
	}
	return nil
}
