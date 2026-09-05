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
	tsWireFile     = "desktop/frontend-next/src/port/wire.ts"
	tsBoundaryFile = "desktop/frontend-next/src/port/boundary.ts"
	tsRemoteFile   = "desktop/frontend-next/src/port/remote.ts"
	tsSessionFile  = "desktop/frontend-next/src/port/session.ts"
	tsVersionFile  = "desktop/frontend-next/src/port/version.ts"
	tsModelFile    = "desktop/frontend-next/src/port/model.ts"
)

// mirroredWireTypes are the Go types the desktop keeps a second, hand-written
// copy of: the graph, whose consumers all fold the same deltas, and the sandbox
// editor, which draws a security posture a field it cannot read would get
// wrong. Declared and not inferred, for the reason the sensitive paths are: no
// spelling tells a mirrored contract from a struct with json tags.
var mirroredWireTypes = []wireMirror{
	{"internal/agentgraph/graph.go", "Node", tsWireFile, "GraphNode"},
	{"internal/agentgraph/graph.go", "Edge", tsWireFile, "GraphEdge"},
	{"internal/agentgraph/graph.go", "Delta", tsWireFile, "GraphDelta"},
	// The snapshot is the execution model's whole authority: no delta carries an
	// interruption or an unrecorded identity, and a watermark the page cannot
	// find is a bootstrap that resumes from zero.
	{"internal/agentgraph/graph.go", "Graph", tsWireFile, "ExecutionGraph"},
	{"internal/control/execution_graph.go", "ExecutionGraphSnapshot", tsWireFile, "ExecutionGraphSnapshot"},
	{"internal/control/execution_graph.go", "ExecutionInterruption", tsWireFile, "ExecutionInterruption"},
	{"internal/serve/executiongraph.go", "executionGraphView", tsWireFile, "ExecutionGraphView"},
	{"internal/control/boundary.go", "SandboxSettings", tsBoundaryFile, "SandboxSettings"},
	// The fold bounds and the one in force: a field the panel cannot read is a
	// threshold a user sets and never sees applied.
	{"internal/control/compaction_settings.go", "CompactionSettings", tsModelFile, "CompactionSettings"},
	// The completion summary is the turn's own verdict on itself; a gap kind the
	// desktop cannot read is a turn it shows as clean.
	{"internal/eventwire/wire.go", "CompletionSummary", tsWireFile, "CompletionSummary"},
	// A maintenance verdict the desktop cannot read is a session that shows no
	// attempt to bound its context — which is what its trajectory export said
	// about the run that motivated the second boundary.
	{"internal/eventwire/wire.go", "ContextMaintenance", tsWireFile, "ContextMaintenance"},
	// Plan rewriting and plan advancement are told apart by three counters; a
	// desktop that can read only some of them reads churn as work.
	{"internal/eventwire/wire.go", "TodoProgress", tsWireFile, "TodoProgress"},
	// RemoteHostEdit is left out on purpose: the kernel still takes the single
	// `workspace` an old row was saved with, which the page deliberately does
	// not send. An under-filled request is not a picture that cannot be read.
	{"internal/serve/hub_remote.go", "RemoteHostView", tsRemoteFile, "RemoteHost"},
	{"internal/serve/remote_browse.go", "RemoteListing", tsRemoteFile, "RemoteListing"},
	{"internal/serve/remote_browse.go", "RemoteFolder", tsRemoteFile, "RemoteFolder"},
	// What waits on the user, and which call answers it. The desktop reads this
	// list as the whole set of open prompts — one it cannot read is a card it
	// seals as decided while the run stays blocked on it.
	{"internal/control/decisions.go", "Decision", tsSessionFile, "Decision"},
	{"internal/control/decisions.go", "DecisionQuestion", tsSessionFile, "DecisionQuestion"},
	{"internal/control/decisions.go", "DecisionOption", tsSessionFile, "DecisionOption"},
	// How a window learns a prompt was answered in another one. A field it
	// cannot read is a card left answerable over a decision already made.
	{"internal/eventwire/wire.go", "DecisionReceipt", tsWireFile, "DecisionReceipt"},
	// What an install in flight is doing. The page polls it and renders one
	// sentence per phase; a phase it cannot read renders as nothing, which on
	// the long pause after the last byte is indistinguishable from a hang.
	{"internal/update/progress.go", "Progress", tsVersionFile, "UpdateProgress"},
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
	{"cmd/e2ebench/main.go", "runMetrics", "internal/cli/run_metrics.go", "RunMetrics"},
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
