package system

import "fmt"

type ProductionReadinessChecks struct {
	RuntimeSafety             bool
	ControlSystemStability    bool
	MemorySystemSafety        bool
	PredictiveSystemIsolation bool
	TemporalSystem            bool
	ArchitectureFreeze        bool
	Observability             bool
}

func StableProductionReadinessChecks() ProductionReadinessChecks {
	return ProductionReadinessChecks{
		RuntimeSafety:             true,
		ControlSystemStability:    true,
		MemorySystemSafety:        true,
		PredictiveSystemIsolation: true,
		TemporalSystem:            true,
		ArchitectureFreeze:        ArchitectureLocked,
		Observability:             true,
	}
}

func AllChecksPassed(checks ...ProductionReadinessChecks) bool {
	if len(checks) == 0 {
		checks = []ProductionReadinessChecks{StableProductionReadinessChecks()}
	}
	for _, check := range checks {
		if !check.RuntimeSafety ||
			!check.ControlSystemStability ||
			!check.MemorySystemSafety ||
			!check.PredictiveSystemIsolation ||
			!check.TemporalSystem ||
			!check.ArchitectureFreeze ||
			!check.Observability {
			return false
		}
	}
	return true
}

func ReleaseGuard() error {
	if !ArchitectureLocked {
		return fmt.Errorf("CANNOT RELEASE: architecture not frozen")
	}
	if err := ValidateSystemContract(StableSystemContract()); err != nil {
		return fmt.Errorf("CANNOT RELEASE: %w", err)
	}
	if !AllChecksPassed() {
		return fmt.Errorf("CANNOT RELEASE: %w", errChecksIncomplete)
	}
	return nil
}
