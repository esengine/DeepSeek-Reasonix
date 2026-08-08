// Package cosplayhook is the pluggable binding between boot and the CoSPlay
// package. boot reads the hooks; cosplay registers them in init. Neither
// imports the other, so PR #7791 can drop the cosplay package and boot still
// compiles (the /code_verify tool and auto-verifier simply absent).
package cosplayhook

import (
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Hooks are the CoSPlay bindings installed by cosplay.init. Zero value = no
// cosplay support in this build.
type Hooks struct {
	// BuildCodeVerifyTool returns the manual /code_verify tool bound to cfg
	// (with a model backend when one is available), or nil to skip.
	BuildCodeVerifyTool func(cfg *config.CosplayConfig, p provider.Provider, base provider.Request) tool.Tool
	// BuildAutoVerifier returns the auto-on-mutation verifier or nil.
	BuildAutoVerifier func(cfg *config.CosplayConfig) agent.CodeVerifier
}

// H holds the installed hooks (registered once by cosplay.init).
var H Hooks

// Register installs the hooks. Call from cosplay's init; at most once.
func Register(h Hooks) { H = h }
