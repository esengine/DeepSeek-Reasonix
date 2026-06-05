// Formats a tool call as a Claude-style card line: a "● Verb(primary arg)"
// header instead of the raw "-> name {json}", plus the "⎿" continuation gutter.
package cli

import (
	"encoding/json"
	"strconv"
	"strings"

	"reasonix/internal/tool"
)

// connector is the Claude-style "⎿" gutter that ties a continuation block (tool
// output, streamed thinking) to the header line above it.
const connector = "  ⎿  "

// connectorBlock renders lines under the connector: the first carries the "⎿"
// gutter, the rest align beneath it. Returns "" for no lines.
func connectorBlock(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	indent := strings.Repeat(" ", len([]rune(connector)))
	out := dim(connector) + lines[0]
	for _, ln := range lines[1:] {
		out += "\n" + indent + ln
	}
	return out
}

// toolVerb maps a tool's snake_case id to the verb shown in its card.
var toolVerb = map[string]string{
	"bash":          "Bash",
	"bash_output":   "Output",
	"kill_shell":    "Kill",
	"wait":          "Wait",
	"read_file":     "Read",
	"write_file":    "Write",
	"edit_file":     "Update",
	"multi_edit":    "Update",
	"delete_range":  "Update",
	"delete_symbol": "Update",
	"notebook_edit": "Update",
	"glob":          "Glob",
	"grep":          "Search",
	"ls":            "List",
	"web_fetch":     "Fetch",
	"web_search":    "Search",
	"complete_step": "Step",
	"task":          "Task",
}

// toolArgKey is the JSON field shown in parentheses for each tool (wait is
// special-cased — it carries a job_ids array, not a scalar).
var toolArgKey = map[string]string{
	"bash":          "command",
	"bash_output":   "job_id",
	"kill_shell":    "job_id",
	"read_file":     "path",
	"write_file":    "path",
	"edit_file":     "path",
	"multi_edit":    "path",
	"delete_range":  "path",
	"delete_symbol": "name",
	"notebook_edit": "path",
	"glob":          "pattern",
	"grep":          "pattern",
	"ls":            "path",
	"web_fetch":     "url",
	"web_search":    "query",
	"complete_step": "summary",
	"task":          "description",
}

// toolDot returns the "●" status glyph coloured by the tool's category so the eye
// can tell reads (cyan) from writes (green), shell (yellow), process control
// (magenta), and everything else (copper) at a glance.
func toolDot(name string) string {
	var c cliColor
	switch toolCategory[name] {
	case "read":
		c = activeCLITheme.toolRead
	case "write":
		c = activeCLITheme.success
	case "exec":
		c = activeCLITheme.warn
	case "proc":
		c = activeCLITheme.toolProc
	default:
		c = activeCLITheme.accent
	}
	return themeFg(c, "●")
}

var toolCategory = map[string]string{
	"read_file": "read", "ls": "read", "glob": "read", "grep": "read",
	"web_fetch": "read", "web_search": "read", "bash_output": "read",
	"write_file": "write", "edit_file": "write", "multi_edit": "write",
	"delete_range": "write", "delete_symbol": "write", "notebook_edit": "write",
	"bash": "exec",
	"wait": "proc", "kill_shell": "proc",
}

// toolDisplayName returns the card verb for a tool: a mapped builtin verb, the
// short name for an MCP tool (mcp__server__tool), or the raw id as a fallback.
func toolDisplayName(name string) string {
	if _, short, ok := tool.SplitMCPName(name); ok {
		return short
	}
	if v, ok := toolVerb[name]; ok {
		return v
	}
	return name
}

// toolArg pulls the primary argument shown in the card's parentheses.
func toolArg(name, args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	if name == "wait" {
		return argList(m["job_ids"])
	}
	v, ok := m[toolArgKey[name]]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		return argList(x)
	case float64:
		return strconv.Itoa(int(x))
	default:
		return ""
	}
}

func argList(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// toolCard renders the dispatch line: "  ⏺ Verb(arg)", arg clamped to width.
func toolCard(name, args string, width int) string {
	return "  " + toolDot(name) + " " + toolHead(name, toolArg(name, args), width)
}

// toolHead builds "Verb(arg)" with the verb bold and the arg clamped to fit the
// remaining width; shared by toolCard and the diff block header.
func toolHead(name, arg string, width int) string {
	label := toolDisplayName(name)
	head := bold(label)
	if arg != "" {
		avail := width - 4 - len([]rune(label)) - 2
		head += dim("(") + clampPlain(arg, avail) + dim(")")
	}
	return head
}
