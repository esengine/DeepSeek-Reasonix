package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/subagentdef"
	"reasonix/internal/tool"
)

var (
	subagentDefMu       sync.Mutex
	subagentDefRegistry *subagentdef.Registry
)

func SetSubagentDefRegistry(r *subagentdef.Registry) {
	subagentDefMu.Lock()
	defer subagentDefMu.Unlock()
	subagentDefRegistry = r
}

func getSubagentDefRegistry() (*subagentdef.Registry, bool) {
	subagentDefMu.Lock()
	defer subagentDefMu.Unlock()
	if subagentDefRegistry == nil {
		return nil, false
	}
	return subagentDefRegistry, true
}

func init() {
	tool.RegisterBuiltin(subagentListTool{})
	tool.RegisterBuiltin(subagentGetTool{})
}

type subagentListTool struct{}

func (subagentListTool) Name() string { return "subagent_list" }

func (subagentListTool) Description() string {
	return "List all available subagent definitions that can be used. Each subagent has a specific role, tool access, and model configuration. Use subagent_get to see full details for a specific subagent."
}

func (subagentListTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "query":{
    "type":"string",
    "description":"Optional search query to filter subagents by name or description."
  }
}
}`)
}

func (subagentListTool) ReadOnly() bool { return true }

func (subagentListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	reg, ok := getSubagentDefRegistry()
	if !ok {
		return "", fmt.Errorf("subagent definitions not available")
	}

	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var defs []*subagentdef.SubagentDefinition
	if p.Query != "" {
		defs = reg.FindByDescription(p.Query)
	} else {
		defs = reg.List()
	}

	if len(defs) == 0 {
		return "No subagent definitions found.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Available subagents (%d total):\n\n", len(defs))
	for _, def := range defs {
		scope := def.SourceScope
		if scope == "" {
			scope = "unknown"
		}
		fmt.Fprintf(&sb, "• %s [%s]\n", def.Name, scope)
		if def.Description != "" {
			desc := def.Description
			if len(desc) > 120 {
				desc = desc[:117] + "..."
			}
			fmt.Fprintf(&sb, "  %s\n", desc)
		}
		if def.Model != "" {
			fmt.Fprintf(&sb, "  Model: %s\n", def.Model)
		}
		if len(def.Tools) > 0 {
			fmt.Fprintf(&sb, "  Tools: %d tools\n", len(def.Tools))
		}
		fmt.Fprintln(&sb)
	}

	return sb.String(), nil
}

type subagentGetTool struct{}

func (subagentGetTool) Name() string { return "subagent_get" }

func (subagentGetTool) Description() string {
	return "Get detailed information about a specific subagent definition, including its full system prompt, tool access, model configuration, and capabilities."
}

func (subagentGetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "name":{
    "type":"string",
    "description":"The name of the subagent definition to retrieve."
  }
},
"required":["name"]
}`)
}

func (subagentGetTool) ReadOnly() bool { return true }

func (subagentGetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	reg, ok := getSubagentDefRegistry()
	if !ok {
		return "", fmt.Errorf("subagent definitions not available")
	}

	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return "", fmt.Errorf("subagent name is required")
	}

	def, ok := reg.Get(p.Name)
	if !ok {
		return "", fmt.Errorf("subagent %q not found", p.Name)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Subagent: %s\n", def.Name)
	fmt.Fprintf(&sb, "Scope: %s\n", def.SourceScope)
	if def.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n", def.Description)
	}
	if def.Model != "" {
		fmt.Fprintf(&sb, "Model: %s\n", def.Model)
	}
	if def.Effort != "" {
		fmt.Fprintf(&sb, "Effort: %s\n", def.Effort)
	}
	if def.PermissionMode != "" {
		fmt.Fprintf(&sb, "Permission mode: %s\n", def.PermissionMode)
	}
	if def.MaxTurns > 0 {
		fmt.Fprintf(&sb, "Max turns: %d\n", def.MaxTurns)
	}
	if def.Isolation != "" {
		fmt.Fprintf(&sb, "Isolation: %s\n", def.Isolation)
	}
	if def.Background {
		fmt.Fprintf(&sb, "Background: yes\n")
	}
	if def.Color != "" {
		fmt.Fprintf(&sb, "Color: %s\n", def.Color)
	}
	if len(def.Tools) > 0 {
		fmt.Fprintf(&sb, "\nAllowed tools (%d):\n", len(def.Tools))
		for _, t := range def.Tools {
			fmt.Fprintf(&sb, "  • %s\n", t)
		}
	}
	if len(def.DisallowedTools) > 0 {
		fmt.Fprintf(&sb, "\nDisallowed tools (%d):\n", len(def.DisallowedTools))
		for _, t := range def.DisallowedTools {
			fmt.Fprintf(&sb, "  • %s\n", t)
		}
	}
	if len(def.Skills) > 0 {
		fmt.Fprintf(&sb, "\nPreloaded skills (%d):\n", len(def.Skills))
		for _, s := range def.Skills {
			fmt.Fprintf(&sb, "  • %s\n", s)
		}
	}
	if def.Prompt != "" {
		prompt := def.Prompt
		if len(prompt) > 500 {
			prompt = prompt[:497] + "..."
		}
		fmt.Fprintf(&sb, "\nSystem prompt:\n%s\n", prompt)
	}
	if def.SourceFile != "" {
		fmt.Fprintf(&sb, "\nSource file: %s\n", def.SourceFile)
	}

	return sb.String(), nil
}
