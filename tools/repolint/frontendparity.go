package main

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

// frontendPort is one frontend package and the composite port it is handed.
// Which port a frontend names is the scope decision this rule measures against,
// so it is declared here rather than read back out of the frontend, where a
// narrowing edit would quietly erase its own debt.
type frontendPort struct {
	pkg  string // repo-relative package directory
	port string // the interface in internal/session/control it drives
}

// A shell that drives the kernel over HTTP is absent: wire-parity covers it.
// internal/frontend/cli is absent for a different reason: with the interactive session
// gone it is a launcher for serve and one-shot run, not a surface a person
// operates a session through, so a capability it does not drive is not debt.
var frontendPorts = []frontendPort{
	{pkg: "internal/frontend/acp", port: "EditorAPI"},
	{pkg: "internal/frontend/serve", port: "SessionAPI"},
}

// frontendScope excuses one capability a frontend's port declares and that it
// deliberately does not drive. The judgement reads method and pkg only; issue
// and reason are required of whoever adds a row and are never matched on. A row
// costs what the missing edge it replaces cost, so it moves debt into this file
// instead of deleting it.
type frontendScope struct {
	method string
	pkg    string
	issue  string
	reason string
}

// Each row is a decision argued in its own pull request.
var frontendScopes = []frontendScope{
	{method: "ReleaseResources", pkg: "internal/frontend/serve", issue: "#13644", reason: "serve swaps and ends controllers through Close; only ACP rebuilds a session in place"},
	{method: "CloseAfterDestroy", pkg: "internal/frontend/serve", issue: "#13644", reason: "serve closes a runtime through Close and does not split delete teardown from the close"},
	{method: "NewSession", pkg: "internal/frontend/acp", issue: "#13644", reason: "an ACP client starts a conversation with session/new; the protocol has no in-place reset"},
	{method: "InstallMCPServer", pkg: "internal/frontend/acp", issue: "#13660", reason: "writes user configuration and adds a program to run; an ACP client passes a session's servers in session/new"},
	{method: "RemoveMCPServer", pkg: "internal/frontend/acp", issue: "#13660", reason: "deletes configuration and its recorded activation, which ACP does not edit"},
	{method: "SetMCPServerEnabled", pkg: "internal/frontend/acp", issue: "#13660", reason: "enabling a project server approves its command and persists; approval belongs where the command is shown"},
	{method: "ClearMCPServerOverride", pkg: "internal/frontend/acp", issue: "#13660", reason: "rewrites the persistent activation record that ACP leaves alone"},
	{method: "SubmitHTTPFormat", pkg: "internal/frontend/acp", issue: "#13644", reason: "applies an HTTP request's response_format; ACP prompts carry no structured-output field"},
	{method: "CustomCommand", pkg: "internal/frontend/serve", issue: "#13662", reason: "the controller's submit path resolves a custom command, so serve inherits it by submitting the line; ACP resolves it itself because its slash handling runs before submit"},
	{method: "MCPPrompt", pkg: "internal/frontend/serve", issue: "#13662", reason: "resolved inside submit, as a custom command is"},
	{method: "EnqueueInbox", pkg: "internal/frontend/serve", issue: "#13662", reason: "serve admits queue items with TryEnqueueFollowup and TryEnqueueAndSteer, which enqueue and dispatch when idle"},
	{method: "TryEnqueueFollowup", pkg: "internal/frontend/acp", issue: "#13662", reason: "ACP enqueues without dispatch and drains the queue itself"},
	{method: "ReplayPendingPromptsWith", pkg: "internal/frontend/acp", issue: "#13662", reason: "an SSE hand-off that closes an attach race; ACP has one stdio connection and no re-attach"},
}

const frontendScopeFile = "tools/repolint/frontendparity.go"

// capability is one exported *Controller method and where it is declared.
type capability struct {
	name string
	file string
	line int
}

// parityMatrix is what the type check saw: every capability the controller
// exports, the ones each frontend's port declares, and the ones it calls.
type parityMatrix struct {
	capabilities []capability
	reachable    map[string]map[string]bool
	consumed     map[string]map[string]bool
}

// REASONIX.md puts behavior on the controller so every frontend inherits it,
// and nothing read that back: serve is handed SessionAPI, which declares
// SwitchBranch, and never calls it. A missing edge is one frontend whose port
// carries a capability another frontend already drives, with no call to it.
func frontendParityFindings(m parityMatrix, ports []frontendPort, scopes []frontendScope, scopeLine int) []Finding {
	// Findings come out in the tool's own order so this function alone is what a
	// test compares against, and so -update writes a stable baseline.
	scoped := map[string]bool{}
	for _, s := range scopes {
		scoped[s.pkg+"."+s.method] = true
	}
	var out []Finding
	for _, c := range m.capabilities {
		// A capability no frontend drives is an internal method, not shared
		// behavior: counting it would make every controller helper a four-way
		// defect. What that costs is locked in frontendparity_test.go.
		if !m.driven(c.name, ports) {
			continue
		}
		row, missing := m.row(c.name, ports, scoped)
		if missing == 0 {
			continue
		}
		out = append(out, Finding{c.file, c.line, ruleFrontendParity,
			fmt.Sprintf("Controller.%s: %s", c.name, row), missing})
	}
	out = append(out, scopeFindings(scopes, scopeLine)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func (m parityMatrix) driven(method string, ports []frontendPort) bool {
	for _, p := range ports {
		if m.consumed[p.pkg][method] {
			return true
		}
	}
	return false
}

// row renders the capability's frontends in declared order and counts the ones
// that could call it and do not. "n/a" is a port that does not carry it, which
// is a scope decision port.go already made and compiles.
func (m parityMatrix) row(method string, ports []frontendPort, scoped map[string]bool) (string, int) {
	cells := make([]string, 0, len(ports))
	missing := 0
	for _, p := range ports {
		name := shortFrontend(p.pkg)
		switch {
		case !m.reachable[p.pkg][method]:
			cells = append(cells, name+"=n/a")
		case m.consumed[p.pkg][method]:
			cells = append(cells, name+"=yes")
		case scoped[p.pkg+"."+method]:
			cells = append(cells, name+"=scoped")
		default:
			cells = append(cells, name+"=no")
			missing++
		}
	}
	return strings.Join(cells, " "), missing
}

func shortFrontend(pkg string) string {
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		return pkg[i+1:]
	}
	return pkg
}

func scopeFindings(scopes []frontendScope, line int) []Finding {
	out := make([]Finding, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, Finding{frontendScopeFile, line, ruleFrontendParity,
			fmt.Sprintf("Controller.%s is scoped away from %s (%s): %s", s.method, s.pkg, s.issue, s.reason), 1})
	}
	return out
}

// A row with no issue or no reason is a malformed declaration rather than debt:
// the judgement never reads either field, so an empty one leaves the next
// reader nothing to overturn.
func validateFrontendScopes(scopes []frontendScope, ports []frontendPort) error {
	known := map[string]bool{}
	for _, p := range ports {
		known[p.pkg] = true
	}
	for _, s := range scopes {
		switch {
		case s.method == "" || s.pkg == "":
			return fmt.Errorf("frontendScopes: a row names no method or no package")
		case !known[s.pkg]:
			return fmt.Errorf("frontendScopes: %s is not a declared frontend", s.pkg)
		case strings.TrimSpace(s.issue) == "" || strings.TrimSpace(s.reason) == "":
			return fmt.Errorf("frontendScopes: %s.%s needs an issue and a reason", s.pkg, s.method)
		}
	}
	return nil
}

// scopeDeclLine finds where frontendScopes is declared, so a scoped row reports
// at the table a reviewer has to open rather than at the top of the file.
func scopeDeclLine(root string) int {
	src, err := parseSource(root, frontendScopeFile)
	if err != nil || src == nil || src.file == nil {
		return 1
	}
	for _, decl := range src.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if name.Name == "frontendScopes" {
					return src.line(name.Pos())
				}
			}
		}
	}
	return 1
}

func checkFrontendParity(root string) ([]Finding, error) {
	if err := validateFrontendScopes(frontendScopes, frontendPorts); err != nil {
		return nil, err
	}
	m, err := loadParityMatrix(root, frontendPorts)
	if err != nil {
		return nil, err
	}
	return frontendParityFindings(m, frontendPorts, frontendScopes, scopeDeclLine(root)), nil
}
