// Package sandbox provides OS-level sandbox confinement and capability
// negotiation for the bash tool. This file adds diagnostic helpers that
// map command output to sandbox_capabilities suggestions.
package sandbox

import (
	"fmt"
	"strings"
)

// diagnosticRule maps a keyword in command output to a suggested capability.
type diagnosticRule struct {
	keyword   string
	capType   string
	suggested string // JSON snippet shown as an example, empty = generic
}

var diagnosticRules = []diagnosticRule{
	{
		keyword:   "couldn't communicate with the NVIDIA driver",
		capType:   "devices",
		suggested: `{"devices":[{"path":"/dev/nvidia0"},{"path":"/dev/nvidiactl"},{"path":"/dev/nvidia-uvm"}]}`,
	},
	{
		keyword:   "NVIDIA-SMI has failed",
		capType:   "devices",
		suggested: `{"devices":[{"path":"/dev/nvidia0"},{"path":"/dev/nvidiactl"},{"path":"/dev/nvidia-uvm"}]}`,
	},
	{
		keyword: "unable to open database",
		capType: "write_paths",
	},
	{
		keyword: "Read-only file system",
		capType: "write_paths",
	},
	{
		keyword: "只读文件系统",
		capType: "write_paths",
	},
	{
		keyword: "no write access",
		capType: "write_paths",
	},
	{
		keyword: "Permission denied",
		capType: "write_paths",
	},
	{
		keyword:   "Connection timed out",
		capType:   "network",
		suggested: `{"network":true}`,
	},
	{
		keyword:   "Temporary failure in name resolution",
		capType:   "network",
		suggested: `{"network":true}`,
	},
}

// SandboxErrorDiagnostic inspects the combined command output for known
// sandbox restriction patterns and returns a formatted diagnostic block.
// It returns empty string when no pattern matches.
func SandboxErrorDiagnostic(output string) string {
	outputLower := strings.ToLower(output)
	for _, rule := range diagnosticRules {
		if !strings.Contains(outputLower, strings.ToLower(rule.keyword)) {
			continue
		}
		var capHint string
		if rule.suggested != "" {
			capHint = fmt.Sprintf("add %q to sandbox_capabilities, e.g. %s", rule.capType, rule.suggested)
		} else {
			capHint = fmt.Sprintf("add %q to sandbox_capabilities", rule.capType)
		}
		return fmt.Sprintf(
			"--- sandbox diagnostic ---\n"+
				"⛔ This error may be caused by sandbox restrictions.\n"+
				"   Matched pattern: %q\n"+
				"   Suggestion: %s\n"+
				"--- end sandbox diagnostic ---",
			rule.keyword, capHint,
		)
	}
	return ""
}
