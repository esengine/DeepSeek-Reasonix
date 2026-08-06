// Command meta-tool-priority demonstrates the config/env priority matrix for
// the [tools] meta_tool setting. It iterates every combination of config value
// (nil / true / false) and env var (unset / 1 / 0 / true / false / on / off /
// empty / garbage), calls the real Config.MCPMetaToolEnabled() resolver, and
// prints a table so the three-layer resolution is visible at a glance.
//
// Usage:
//
//	go run ./cmd/meta-tool-priority/
package main

import (
	"fmt"
	"os"
	"strings"

	"reasonix/internal/config"
)

// envCase is one REASONIX_MCP_META_TOOL scenario.
type envCase struct {
	label string
	value string // raw value to Setenv; when unset is true, Unsetenv is used
	unset bool   // true = Unsetenv, false = Setenv(value)
}

// cfgCase is one [tools] meta_tool config scenario.
type cfgCase struct {
	label string
	value *bool
}

func main() {
	orig, hadOrig := os.LookupEnv("REASONIX_MCP_META_TOOL")
	defer func() {
		if hadOrig {
			os.Setenv("REASONIX_MCP_META_TOOL", orig)
		} else {
			os.Unsetenv("REASONIX_MCP_META_TOOL")
		}
	}()

	cfgCases := []cfgCase{
		{"nil (unset)", nil},
		{"true", boolPtr(true)},
		{"false", boolPtr(false)},
	}

	envCases := []envCase{
		{"unset", "", true},
		{"\"1\"", "1", false},
		{"\"0\"", "0", false},
		{"\"true\"", "true", false},
		{"\"false\"", "false", false},
		{"\"on\"", "on", false},
		{"\"off\"", "off", false},
		{"\"yes\"", "yes", false},
		{"\"no\"", "no", false},
		{"\"\" (empty)", "", false},
		{"\"maybe\" (garbage)", "maybe", false},
		{"\"TRUE\" (upper)", "TRUE", false},
		{"\"  on  \" (spaces)", "  on  ", false},
	}

	fmt.Println("=== meta_tool priority matrix ===")
	fmt.Println("Resolution order: env var > [tools] meta_tool config > default false")
	fmt.Println()
	fmt.Printf("%-22s │ %-20s │ %-8s │ %s\n", "[tools] meta_tool", "REASONIX_MCP_META_TOOL", "result", "source")
	fmt.Println(strings.Repeat("─", 80))

	for _, cc := range cfgCases {
		for _, ec := range envCases {
			setEnv(ec)
			cfg := &config.Config{}
			if cc.value != nil {
				v := *cc.value
				cfg.Tools.MetaTool = &v
			}
			result := cfg.MCPMetaToolEnabled()
			source := resolveSource(cc, ec, result)
			fmt.Printf("%-22s │ %-20s │ %-8s │ %s\n", cc.label, ec.label, fmtResult(result), source)
		}
		fmt.Println(strings.Repeat("─", 80))
	}

	fmt.Println()
	fmt.Println("Legend:")
	fmt.Println("  result = the value boot.go sees from cfg.MCPMetaToolEnabled()")
	fmt.Println("  source = which layer won:")
	fmt.Println("    env-on   = env var enabled (overrides everything)")
	fmt.Println("    env-off  = env var disabled (overrides everything)")
	fmt.Println("    config   = [tools] meta_tool config value used (env unset/empty/garbage)")
	fmt.Println("    default  = neither env nor config set anything → false (legacy behavior)")
}

func setEnv(ec envCase) {
	if ec.unset {
		os.Unsetenv("REASONIX_MCP_META_TOOL")
	} else {
		os.Setenv("REASONIX_MCP_META_TOOL", ec.value)
	}
}

// resolveSource determines which resolution layer produced the result.
func resolveSource(cc cfgCase, ec envCase, result bool) string {
	// Check if env var is a recognized boolean spelling.
	v := strings.ToLower(strings.TrimSpace(ec.value))
	if !ec.unset {
		switch v {
		case "1", "true", "yes", "on":
			return "env-on"
		case "0", "false", "no", "off":
			return "env-off"
		}
	}
	// Env didn't override → config or default.
	if cc.value != nil {
		return "config"
	}
	return "default"
}

func boolPtr(b bool) *bool { return &b }

func fmtResult(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
