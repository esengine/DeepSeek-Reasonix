package boot

import (
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

// Automatic selection must not move an otherwise usable task into an environment
// whose program tools cannot run. This is only a preparation check: each launch
// still validates the final writable surface and its actual confinement backend.
func automaticIsolationAvailability(parent Options, cfg *config.Config) error {
	if cfg == nil {
		return tool.Refusal{Code: isolation.CodeUnavailable, Message: "automatic isolation requires frozen host configuration"}
	}
	network := cfg.Sandbox.Network
	if parent.SandboxNetworkOverride != nil {
		network = *parent.SandboxNetworkOverride
	}
	spec := sandbox.Spec{Mode: "enforce", RequiredIntegrity: sandbox.CompleteFileIntegrity, Network: network}
	if !sandbox.AvailableFor(spec) {
		return tool.Refusal{Code: isolation.CodeUnavailable, Message: "automatic isolation cannot prepare program execution: " + sandbox.UnavailableMessageFor(spec)}
	}
	return nil
}

func checkAutomaticTaskIsolation(parent Options, specMode, resumeID string) error {
	if resumeID != "" {
		return nil
	}
	mode := strings.TrimSpace(specMode)
	if mode == "" {
		mode, _ = parent.frozenConfig.CollaborationMode()
	}
	if mode == "auto" {
		return automaticIsolationAvailability(parent, parent.frozenConfig)
	}
	return nil
}

func (b *builder) writeCoordination(opts Options, cfg *config.Config) *control.WriteCoordinationRuntime {
	runtime := control.WriteCoordinationFromConfig(cfg, b.session.lease, b.tools.sub.scheduler)
	if runtime != nil && runtime.Collaboration == "auto" {
		runtime.AutomaticIsolationFallback = automaticIsolationAvailability(opts, cfg) != nil
	}
	return runtime
}
