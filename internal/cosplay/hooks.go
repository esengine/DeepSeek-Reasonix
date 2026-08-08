package cosplay

import (
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/cosplayhook"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// init installs the CoSPlay bindings into cosplayhook.H so boot can use the
// manual /code_verify tool and the auto-on-mutation verifier without importing
// this package (PR #7791 can drop cosplay and boot still compiles).
func init() {
	cosplayhook.Register(cosplayhook.Hooks{
		BuildCodeVerifyTool: buildCodeVerifyTool,
		BuildAutoVerifier:   buildAutoVerifier,
	})
}

// buildCodeVerifyTool is the hook implementation: it mirrors the former boot
// wiring (parameters from config, model-backed gen/repair when enabled).
func buildCodeVerifyTool(cfg *config.CosplayConfig, p provider.Provider, base provider.Request) tool.Tool {
	vt := NewCodeVerifyTool()
	if cfg != nil {
		if cfg.MaxRounds > 0 {
			vt.MaxRounds = cfg.MaxRounds
		}
		if cfg.NumTests > 0 {
			vt.NumTests = cfg.NumTests
		}
		if cfg.TimeoutSeconds > 0 {
			vt.Timeout = cfg.TimeoutSeconds
		}
	}
	if cfg != nil && cfg.Enabled {
		if backend := NewModelBackend(p, base); backend != nil {
			vt.Gen = ModelGenerator{Backend: backend}
			vt.Repair = ModelRepairer{Backend: backend}
		}
	}
	return vt
}

// buildAutoVerifier is the hook implementation for auto-on-mutation.
func buildAutoVerifier(cfg *config.CosplayConfig) agent.CodeVerifier {
	return NewAutoVerifier(AutoConfig{
		NumTests:    cfg.AutoNumTests,
		MaxRounds:   cfg.AutoMaxRounds,
		Timeout:     time.Duration(cfg.AutoTimeoutSeconds) * time.Second,
		Concurrency: 1,
	})
}
