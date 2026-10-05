package evidence

import "strings"

// Non-mutating meta tools spawn or inspect work without themselves changing
// workspace state. Real mutations are recorded from child evidence or the
// underlying target tool after resolution (use_capability call).
var nonMutationMetaTools = map[string]bool{
	"run_skill":           true,
	"read_skill":          true,
	"read_only_skill":     true,
	"task":                true,
	"read_only_task":      true,
	"parallel_tasks":      true,
	"explore":             true,
	"locate":              true,
	"research":            true,
	"review":              true,
	"security_review":     true,
	"use_capability":      true,
	"connect_tool_source": true,
	"review_report":       true,
}

// IsNonMutationMetaTool reports whether toolName is a host meta tool that must
// not count as a mutation receipt by itself.
func IsNonMutationMetaTool(toolName string) bool {
	return nonMutationMetaTools[strings.TrimSpace(toolName)]
}
