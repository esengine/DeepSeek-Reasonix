package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	_ "embed"

	"reasonix/internal/tool"
)

//go:embed widget-readme.md
var widgetDesignSpec string

func init() { tool.RegisterBuiltin(widgetReadme{}) }

// widgetReadme returns the Widget Renderer's design specification — the full
// set of visual rules, color palettes, layout guidelines, animation specs, and
// use-case templates — so the LLM can produce polished, theme-consistent
// widgets. Call BEFORE outputting a ```widget code block.
type widgetReadme struct{}

func (widgetReadme) Name() string { return "widget_readme" }

func (widgetReadme) Description() string {
	return "Load the widget design specification. Call this BEFORE outputting a ```widget code block to get the complete design rules, color palette, animation specs, and layout guidelines. The output contains all the information needed to create polished, theme-consistent HTML/SVG widgets — philosophy, color system, SVG setup, UI components, diagram types, Chart.js integration, and animation specifications. Pass modules to load only what you need (default: core).\n\nIMPORTANT: You MUST output the widget HTML inside a ```widget fenced code block directly in your message. Do NOT use write_file, bash echo, or any other tool to write widget HTML to a file — the frontend only renders ```widget code blocks in message text."
}

func (widgetReadme) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "modules":{
    "type":"array",
    "items":{"type":"string","enum":["core","diagram","chart","mockup","interactive","art"]},
    "description":"Optional. Which module sections to load. 'core' is always included. Available modules: diagram = flowchart/structural/illustrative diagram specs, chart = Chart.js integration rules, mockup = UI component/mockup templates, interactive = interactive explainer templates, art = generative art rules. Default: ['core']"
  }
},
"required":[]
}`)
}

func (widgetReadme) ReadOnly() bool { return true }

func (widgetReadme) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Modules []string `json:"modules,omitempty"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}

	// Always include core
	hasCore := false
	for _, m := range p.Modules {
		if m == "core" {
			hasCore = true
			break
		}
	}
	if !hasCore {
		p.Modules = append([]string{"core"}, p.Modules...)
	}

	return filterSpec(widgetDesignSpec, p.Modules), nil
}

// sectionHeadings maps module names to the ##-level section headings they
// should include. Once a heading prefix matches, everything until the next
// ## heading (or EOF) is included in the result.
var sectionHeadings = map[string][]string{
	"core":        {"Core Design System", "Color palette", "SVG setup", "UI components"},
	"diagram":     {"Diagram types"},
	"chart":       {"Charts"},
	"interactive": {"Interactive explainer", "Compare options", "Data record"},
	"mockup":      {"UI components", "Data record"},
	"art":         {"Art and illustration"},
}

// filterSpec extracts sections from the spec markdown that match the requested
// module names. It walks by ## and ###-level headings.
func filterSpec(spec string, modules []string) string {
	wanted := map[string]bool{}
	for _, mod := range modules {
		if heads, ok := sectionHeadings[mod]; ok {
			for _, h := range heads {
				wanted[h] = true
			}
		}
	}

	lines := strings.Split(spec, "\n")
	var b strings.Builder
	inSection := false

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			heading := strings.TrimLeft(line, "# ")
			matched := false
			for w := range wanted {
				// Support prefix matching so "Charts" matches "Charts (Chart.js)"
				if strings.HasPrefix(heading, w) || strings.HasPrefix(w, heading) ||
					strings.Contains(strings.ToLower(heading), strings.ToLower(w)) {
					matched = true
					break
				}
			}
			inSection = matched
		}
		if inSection {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	// Remove trailing blank lines
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return "No matching sections found. Available modules: core, diagram, chart, mockup, interactive, art. Try 'widget_readme {\"modules\":[\"core\"]}' to load the base design spec."
	}
	return out
}
