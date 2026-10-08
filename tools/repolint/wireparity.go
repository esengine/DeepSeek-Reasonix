package main

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// Where the desktop re-declares the kernel's wire types by hand.
const (
	tsWireFile      = "desktop/frontend-next/src/port/wire.ts"
	tsBoundaryFile  = "desktop/frontend-next/src/port/boundary.ts"
	tsRemoteFile    = "desktop/frontend-next/src/port/remote.ts"
	tsSessionFile   = "desktop/frontend-next/src/port/session.ts"
	tsVersionFile   = "desktop/frontend-next/src/port/version.ts"
	tsModelFile     = "desktop/frontend-next/src/port/model.ts"
	tsMcpFile       = "desktop/frontend-next/src/port/mcp.ts"
	tsShellFile     = "desktop/frontend-next/src/port/shell.ts"
	tsShareFile     = "desktop/frontend-next/src/port/share.ts"
	tsProviderFile  = "desktop/frontend-next/src/port/provider.ts"
	tsFeedbackFile  = "desktop/frontend-next/src/port/feedback.ts"
	tsWorkspaceFile = "desktop/frontend-next/src/port/workspace.ts"
	tsHubFile       = "desktop/frontend-next/src/port/hub.ts"
	tsChartFile     = "desktop/frontend-next/src/ui/chart/spec.ts"
)

// mirroredWireTypes are the Go types the desktop keeps a second, hand-written
// copy of: the graph, whose consumers all fold the same deltas, and the sandbox
// editor, which draws a security posture a field it cannot read would get
// wrong. Declared and not inferred, for the reason the sensitive paths are: no
// spelling tells a mirrored contract from a struct with json tags.
var mirroredWireTypes = []wireMirror{
	{"internal/contract/eventwire/wire.go", "AskOrigin", tsWireFile, "AskOrigin"},
	{"internal/contract/agentgraph/graph.go", "Node", tsWireFile, "GraphNode"},
	{"internal/contract/agentgraph/graph.go", "Edge", tsWireFile, "GraphEdge"},
	{"internal/contract/agentgraph/graph.go", "Delta", tsWireFile, "GraphDelta"},
	// The snapshot is the execution model's whole authority: no delta carries an
	// interruption or an unrecorded identity, and a watermark the page cannot
	// find is a bootstrap that resumes from zero.
	{"internal/contract/agentgraph/graph.go", "Graph", tsWireFile, "ExecutionGraph"},
	{"internal/session/control/execution_graph.go", "ExecutionGraphSnapshot", tsWireFile, "ExecutionGraphSnapshot"},
	{"internal/session/control/execution_graph.go", "ExecutionInterruption", tsWireFile, "ExecutionInterruption"},
	{"internal/frontend/serve/executiongraph.go", "executionGraphView", tsWireFile, "ExecutionGraphView"},
	// Whether a replayed trajectory covers the session or only a prefix of it.
	// A page that cannot read the coverage is back to the empty array that meant
	// three different things.
	{"internal/frontend/serve/wirelog.go", "trajectoryView", tsWireFile, "TrajectoryRead"},
	// The tool frame itself. Who issued a call is the one thing separating the
	// model's work from the host's own bookkeeping, and a page that cannot read
	// it folds them together.
	{"internal/contract/eventwire/wire.go", "Tool", tsWireFile, "Tool"},
	// The canonical task list, which the panel reads rather than derives. A
	// status the page cannot read is a signed-off step drawn as the current one.
	{"internal/frontend/serve/todos.go", "todoItem", tsSessionFile, "HostTodo"},
	// The agent's tabs. The target id is what the window draws a view by, so a
	// tab the page cannot read one for is a tab it cannot show.
	{"internal/platform/browser/session.go", "TabInfo", tsSessionFile, "BrowserTab"},
	// A rebuilt transcript's tool calls. The proxy's resolved identity was on
	// the kernel side and not on this one, so a reopened session drew
	// use_capability where the live one drew what it reached.
	{"internal/frontend/serve/history.go", "historyToolCall", tsSessionFile, "HistoryToolCall"},
	// Who can reach this window from another device. A device the page cannot
	// list is one the person at the window cannot see to disconnect.
	{"internal/frontend/serve/device_share.go", "ShareStatus", tsShareFile, "ShareStatus"},
	{"internal/frontend/serve/device_share.go", "ShareAddress", tsShareFile, "ShareAddress"},
	{"internal/frontend/serve/device_share.go", "ShareOffer", tsShareFile, "ShareOffer"},
	{"internal/frontend/serve/device_registry.go", "DeviceView", tsShareFile, "PairedDevice"},
	{"internal/frontend/serve/device_gate.go", "DeviceSelf", tsShareFile, "DeviceSelf"},
	{"internal/session/control/boundary.go", "SandboxSettings", tsBoundaryFile, "SandboxSettings"},
	{"internal/session/control/browser_settings.go", "BrowserToolsSettings", tsBoundaryFile, "BrowserToolsSettings"},
	// The MCP row: a status the host answered with and the page cannot read is a
	// row that goes back to reading the server's own prose for it.
	{"internal/frontend/serve/catalog.go", "mcpEntry", tsMcpFile, "McpEntry"},
	// The fold bounds and the one in force: a field the panel cannot read is a
	// threshold a user sets and never sees applied.
	{"internal/session/control/compaction_settings.go", "CompactionSettings", tsModelFile, "CompactionSettings"},
	{"internal/session/control/model_modes.go", "ModelModeView", tsModelFile, "ModelMode"},
	// The completion summary is the turn's own verdict on itself; a gap kind the
	// desktop cannot read is a turn it shows as clean.
	{"internal/contract/eventwire/wire.go", "CompletionSummary", tsWireFile, "CompletionSummary"},
	// A maintenance verdict the desktop cannot read is a session that shows no
	// attempt to bound its context — which is what its trajectory export said
	// about the run that motivated the second boundary.
	{"internal/contract/eventwire/wire.go", "ContextMaintenance", tsWireFile, "ContextMaintenance"},
	// The gauge and the two ceilings behind it. A panel that cannot read which
	// bound fires draws a fold point at 16% of the declared window with nothing
	// on the screen that explains it, which is what it read as.
	{"internal/frontend/serve/context_window.go", "contextView", tsShellFile, "ContextBreakdown"},
	// What sent one fold. Without the boundary and its size the card falls back
	// to "a threshold was reached", and the reader supplies their own reason.
	{"internal/contract/eventwire/wire.go", "Compaction", tsWireFile, "Compaction"},
	// Plan rewriting and plan advancement are told apart by three counters; a
	// desktop that can read only some of them reads churn as work.
	{"internal/contract/eventwire/wire.go", "TodoProgress", tsWireFile, "TodoProgress"},
	// A stall report a desktop reads only half of says "stalled" without the
	// count or the cause, and the reader invents one.
	{"internal/contract/eventwire/progress_watch.go", "ProgressWatch", tsWireFile, "ProgressWatch"},
	{"internal/session/control/display_currency.go", "DisplayCurrencySettings", tsBoundaryFile, "DisplayCurrencySettings"},
	{"internal/session/control/progress_watch_settings.go", "ProgressWatchSettings", tsBoundaryFile, "ProgressWatchSettings"},
	// RemoteHostEdit is left out on purpose: the kernel still takes the single
	// `workspace` an old row was saved with, which the page deliberately does
	// not send. An under-filled request is not a picture that cannot be read.
	{"internal/frontend/serve/hub_remote.go", "RemoteHostView", tsRemoteFile, "RemoteHost"},
	{"internal/frontend/serve/remote_browse.go", "RemoteListing", tsRemoteFile, "RemoteListing"},
	{"internal/frontend/serve/remote_browse.go", "RemoteFolder", tsRemoteFile, "RemoteFolder"},
	// The sidebar row. The unread mark is derived by the kernel from two stored
	// timestamps; a row the page cannot read it from shows a finished turn as seen.
	{"internal/frontend/serve/hub_tree.go", "treeSession", tsHubFile, "TreeSession"},
	// What waits on the user, and which call answers it. The desktop reads this
	// list as the whole set of open prompts — one it cannot read is a card it
	// seals as decided while the run stays blocked on it.
	{"internal/session/control/decisions.go", "Decision", tsSessionFile, "Decision"},
	{"internal/session/control/decisions.go", "DecisionQuestion", tsSessionFile, "DecisionQuestion"},
	{"internal/session/control/decisions.go", "DecisionOption", tsSessionFile, "DecisionOption"},
	// How a window learns a prompt was answered in another one. A field it
	// cannot read is a card left answerable over a decision already made.
	{"internal/contract/eventwire/wire.go", "DecisionReceipt", tsWireFile, "DecisionReceipt"},
	// What an install in flight is doing. The page polls it and renders one
	// sentence per phase; a phase it cannot read renders as nothing, which on
	// the long pause after the last byte is indistinguishable from a hang.
	{"internal/platform/update/progress.go", "Progress", tsVersionFile, "UpdateProgress"},
	// The panel lists these and offers a notes view only for a row that says it
	// has them; a field it cannot read is a row that never offers one.
	{"internal/platform/update/hub.go", "VersionEntry", tsVersionFile, "VersionEntry"},
	{"internal/platform/update/hub.go", "VersionHub", tsVersionFile, "VersionHub"},
	{"internal/platform/update/notes.go", "VersionNotes", tsVersionFile, "VersionNotes"},
	{"internal/state/checkpoint/types.go", "RewindResult", tsSessionFile, "RewindResult"},
	{"internal/state/checkpoint/types.go", "RewindUndo", tsSessionFile, "RewindUndo"},
	{"internal/contract/eventwire/wire.go", "ShellExecution", tsWireFile, "Execution"},
	{"internal/contract/eventwire/workspace_lease.go", "WorkspaceLease", tsWireFile, "WorkspaceLease"},
	// A source and a model as the pickers name them. A label the page cannot
	// read puts the config name back on screen after the user renamed it.
	{"internal/frontend/serve/providers.go", "providerView", tsProviderFile, "ProviderEntry"},
	{"internal/frontend/serve/provider_check.go", "providerCheck", tsProviderFile, "ProviderCheck"},
	{"internal/frontend/serve/provider_check.go", "providerModelCheck", tsProviderFile, "ProviderModelCheck"},
	{"internal/frontend/serve/settings.go", "modelEntry", tsModelFile, "ModelEntry"},
	// A per-profile entry that outranks a role's global model. A scope the page
	// cannot read offers a clear the kernel will refuse.
	{"internal/frontend/serve/roles.go", "roleOverride", tsModelFile, "RoleOverride"},
	// The report form draws what the kernel will send and reads back what
	// became of it; a field the page cannot read is a status it cannot show.
	{"internal/platform/feedback/types.go", "Env", tsFeedbackFile, "FeedbackEnvInfo"},
	{"internal/platform/feedback/types.go", "Limits", tsFeedbackFile, "FeedbackLimits"},
	{"internal/platform/feedback/types.go", "Receipt", tsFeedbackFile, "FeedbackReceipt"},
	{"internal/platform/feedback/types.go", "Item", tsFeedbackFile, "FeedbackItem"},
	{"internal/platform/feedback/types.go", "Reply", tsFeedbackFile, "FeedbackReply"},
	{"internal/platform/feedback/types.go", "ReplyReceipt", tsFeedbackFile, "FeedbackReplyReceipt"},
	{"internal/platform/feedback/types.go", "Mine", tsFeedbackFile, "FeedbackMine"},
	{"internal/platform/feedback/types.go", "Profile", tsFeedbackFile, "FeedbackProfile"},
	{"internal/platform/feedback/types.go", "EffectiveLimits", tsFeedbackFile, "FeedbackEffectiveLimits"},
	{"internal/frontend/serve/feedback.go", "feedbackEnvView", tsFeedbackFile, "FeedbackEnv"},
	{"internal/frontend/serve/feedback.go", "feedbackSubmitBody", tsFeedbackFile, "FeedbackRequest"},
	{"internal/frontend/serve/feedback.go", "feedbackImageBody", tsFeedbackFile, "FeedbackImage"},
	// The staged set a commit message was drafted for. The fingerprint is what
	// the confirmation is checked against, so a page that cannot read it cannot
	// confirm anything.
	{"internal/session/control/commit.go", "CommitProposal", tsWorkspaceFile, "CommitProposal"},
	{"internal/session/control/commit.go", "CommitRequest", tsWorkspaceFile, "CommitRequest"},
	{"internal/session/control/commit.go", "CommitResult", tsWorkspaceFile, "CommitResult"},
	{"internal/platform/gitcommit/gitcommit.go", "File", tsWorkspaceFile, "CommitFile"},
	// The chart spec a stored render_chart call carries. The page re-validates it
	// on load, so a field it cannot read is one the card silently drops.
	{"internal/contract/chartspec/spec.go", "Spec", tsChartFile, "ChartSpec"},
	{"internal/contract/chartspec/spec.go", "Data", tsChartFile, "ChartData"},
	{"internal/contract/chartspec/spec.go", "Column", tsChartFile, "ChartColumn"},
	{"internal/contract/chartspec/spec.go", "Mark", tsChartFile, "ChartMark"},
	{"internal/contract/chartspec/spec.go", "Axis", tsChartFile, "ChartAxis"},
}

type wireMirror struct {
	goFile, goType, tsFile, tsType string
}

// readMirrors are Go types that read another Go type's JSON by hand. Unlike the
// desktop's pictures these are read-only and deliberately partial — a harness
// charts the few numbers it compares — so only one direction is a defect: a
// name the reader declares and the writer never sends reads as zero forever,
// which is what a rename on the writing side looks like from here.
var readMirrors = []readMirror{
	{"cmd/e2ebench/main.go", "runMetrics", "internal/frontend/cli/run_metrics.go", "RunMetrics"},
}

type readMirror struct {
	readerFile, readerType, writerFile, writerType string
}

// wireScan collects the declared types' wire field names while the tree is
// walked, so the comparison costs no second parse.
type wireScan struct{ fields map[string][]string }

func newWireScan() *wireScan { return &wireScan{fields: map[string][]string{}} }

func (w *wireScan) observe(src *sourceFile) {
	if src.file == nil {
		return
	}
	for _, m := range mirroredWireTypes {
		if m.goFile != src.rel {
			continue
		}
		if names, ok := wireFieldNames(src.file, m.goType); ok {
			w.fields[m.goFile+"."+m.goType] = names
		}
	}
	for _, m := range readMirrors {
		for _, side := range [][2]string{{m.readerFile, m.readerType}, {m.writerFile, m.writerType}} {
			if side[0] != src.rel {
				continue
			}
			if names, ok := wireFieldNames(src.file, side[1]); ok {
				w.fields[side[0]+"."+side[1]] = names
			}
		}
	}
}

// findings compares each declared pair both ways. Either direction is a defect:
// a picture cannot show what it was never told, and it cannot expect what
// nothing sends.
func (w *wireScan) findings(root string) []Finding {
	bodies := map[string]string{}
	for _, m := range mirroredWireTypes {
		if _, read := bodies[m.tsFile]; read {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, m.tsFile))
		if err != nil {
			return nil
		}
		bodies[m.tsFile] = string(raw)
	}
	return append(wireParityFindings(mirroredWireTypes, w.fields, bodies),
		readParityFindings(readMirrors, w.fields)...)
}

// readParityFindings checks the one direction a hand-written reader owes: every
// name it declares must be one the writer actually sends. What the writer sends
// and this reader ignores is the point of a partial mirror, so it is not a
// finding.
func readParityFindings(mirrors []readMirror, goFields map[string][]string) []Finding {
	var out []Finding
	for _, m := range mirrors {
		reader, declared := goFields[m.readerFile+"."+m.readerType]
		writer, sends := goFields[m.writerFile+"."+m.writerType]
		if !declared || !sends {
			missing, at := m.writerType, m.writerFile
			if !declared {
				missing, at = m.readerType, m.readerFile
			}
			out = append(out, Finding{at, 1, ruleWireParity,
				fmt.Sprintf("%s is declared a mirrored read contract and is not a struct here", missing), 1})
			continue
		}
		for _, name := range missingFrom(reader, writer) {
			out = append(out, Finding{m.readerFile, 1, ruleWireParity,
				fmt.Sprintf("%s reads %q and %s.%s never sends it", m.readerType, name, m.writerFile, m.writerType), 1})
		}
	}
	return out
}

// wireParityFindings is the comparison itself, so a fixture can carry the one
// pair a case is about rather than the tree's real ones.
func wireParityFindings(mirrors []wireMirror, goFields map[string][]string, bodies map[string]string) []Finding {
	var out []Finding
	for _, m := range mirrors {
		fields, declared := goFields[m.goFile+"."+m.goType]
		if !declared {
			out = append(out, Finding{m.goFile, 1, ruleWireParity,
				fmt.Sprintf("%s is declared a mirrored wire type and is not a struct here", m.goType), 1})
			continue
		}
		tsFields, line, found := tsInterfaceFields(bodies[m.tsFile], m.tsType)
		if !found {
			out = append(out, Finding{m.tsFile, 1, ruleWireParity,
				fmt.Sprintf("%s mirrors %s.%s and is not declared here", m.tsType, m.goFile, m.goType), 1})
			continue
		}
		for _, name := range missingFrom(fields, tsFields) {
			out = append(out, Finding{m.tsFile, line, ruleWireParity,
				fmt.Sprintf("%s sends %q and %s cannot read it", m.goType, name, m.tsType), 1})
		}
		for _, name := range missingFrom(tsFields, fields) {
			out = append(out, Finding{m.tsFile, line, ruleWireParity,
				fmt.Sprintf("%s reads %q and %s never sends it", m.tsType, name, m.goType), 1})
		}
	}
	return out
}

func missingFrom(want, have []string) []string {
	var out []string
	for _, name := range want {
		if !slices.Contains(have, name) {
			out = append(out, name)
		}
	}
	return out
}

// wireFieldNames lists what this struct serialises as. A field the encoder skips
// is not part of the contract and is not reported.
func wireFieldNames(file *ast.File, typeName string) ([]string, bool) {
	st, ok := structNamed(file, typeName)
	if !ok {
		return nil, false
	}
	var out []string
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			if !name.IsExported() {
				continue
			}
			if wire, keep := wireName(field, name.Name); keep {
				out = append(out, wire)
			}
		}
	}
	return out, true
}

func structNamed(file *ast.File, typeName string) (*ast.StructType, bool) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != typeName {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
				return st, true
			}
		}
	}
	return nil, false
}

func wireName(field *ast.Field, goName string) (string, bool) {
	if field.Tag == nil {
		return goName, true
	}
	tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json")
	if tag == "-" {
		return "", false
	}
	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name, true
	}
	return goName, true
}

var (
	tsInterfaceRe = regexp.MustCompile(`(?m)^export interface (\w+) \{`)
	tsPropertyRe  = regexp.MustCompile(`^\s*(\w+)\??:`)
)

// tsInterfaceFields reads one interface's property names and the line it opens
// on. It reads the declaration's shape, never its wording, which is all a
// contract is.
func tsInterfaceFields(body, typeName string) ([]string, int, bool) {
	for _, m := range tsInterfaceRe.FindAllStringSubmatchIndex(body, -1) {
		if body[m[2]:m[3]] != typeName {
			continue
		}
		fields, _, ok := strings.Cut(body[m[1]:], "\n}")
		if !ok {
			return nil, 0, false
		}
		var out []string
		for line := range strings.SplitSeq(fields, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if p := tsPropertyRe.FindStringSubmatch(line); p != nil {
				out = append(out, p[1])
			}
		}
		return out, strings.Count(body[:m[0]], "\n") + 1, true
	}
	return nil, 0, false
}
