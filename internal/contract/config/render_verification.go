package config

import (
	"fmt"
	"strings"
)

func renderVerificationRuntime(out *strings.Builder, cfg *Config, scope RenderScope) {
	if scope != RenderScopeProject && cfg.Agent.AutoIntegrate != nil {
		fmt.Fprintf(out, "auto_integrate = %t   # publish verified conflict-free results automatically; user file only\n", *cfg.Agent.AutoIntegrate)
	}
	if scope != RenderScopeProject && cfg.Agent.VerificationRuntime != "" {
		fmt.Fprintf(out, "verification_runtime = %q   # trusted offline Linux root or docker-image://sha256:<id>; user file only\n", cfg.Agent.VerificationRuntime)
	}
}
