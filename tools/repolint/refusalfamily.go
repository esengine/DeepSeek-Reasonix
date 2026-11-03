package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// The two sides of one contract, declared rather than inferred: no spelling
// tells an error family that must reach a frontend from one that stays inside
// the kernel, and no spelling tells the writer that carries an identity from
// one that carries a sentence.
const (
	inboxFamilyFile        = "internal/state/sessioninbox/types.go"
	sessionLeaseFamilyFile = "internal/state/sessionstore/session_lease.go"

	// Both frontends answer inbox calls from the same family, so both writers
	// are named here. A writer that moved would otherwise compare clean against
	// an empty set, which is the failure this check exists to catch.
	inboxHTTPWriterFile = "internal/frontend/serve/inbox.go"
	inboxHTTPWriterFunc = "writeInboxError"
	inboxACPWriterFile  = "internal/frontend/acp/inbox.go"
	inboxACPWriterFunc  = "inboxRefusal"

	sessionLeaseACPWriterFile = "internal/frontend/acp/session.go"
	sessionLeaseACPWriterFunc = "sessionLeaseRefusal"
)

// refusalFamily is one store package's exported error family: the file that
// declares it and the selector a frontend switch names it by.
type refusalFamily struct {
	file string
	pkg  string
}

// refusalWriter is one frontend's refusal writer and the calls that carry an
// identity in that frontend's shape: the HTTP helpers take the dotted code as an
// argument, and the ACP one returns it as data beside the JSON-RPC code.
type refusalWriter struct {
	file string
	fn   string
	// coded names the functions a case must call, one argument of which is the
	// dotted identity. A code assembled at runtime is one neither side can read.
	coded map[string]bool
}

// The writers that give a refusal a dotted code. writeErr is deliberately not
// among them: it renders an identity a deeper layer assigned and otherwise
// falls back to prose, which is the diagnostic path, not an answer a frontend
// can say in the reader's language.
var (
	inboxFamily = refusalFamily{file: inboxFamilyFile, pkg: "sessioninbox"}
	leaseFamily = refusalFamily{file: sessionLeaseFamilyFile, pkg: "sessionstore"}
)

var inboxWriters = []refusalWriter{
	{
		file:  inboxHTTPWriterFile,
		fn:    inboxHTTPWriterFunc,
		coded: map[string]bool{"refuse": true, "busy": true, "refusal": true, "Refusal": true, "saveFailed": true},
	},
	{
		file:  inboxACPWriterFile,
		fn:    inboxACPWriterFunc,
		coded: map[string]bool{"refuseInbox": true},
	},
}

// A lease refusal is a transport answer too: the session methods take the
// transcript's lease, and a client that cannot tell "held" from "unprovable"
// cannot decide whether to close a window or retry.
var sessionLeaseWriters = []refusalWriter{
	{
		file:  sessionLeaseACPWriterFile,
		fn:    sessionLeaseACPWriterFunc,
		coded: map[string]bool{"refuseSessionLease": true},
	},
}

// checkInboxRefusalParity holds each frontend's inbox boundary to the store's
// error family, through the same mechanism the lease family uses.
func checkInboxRefusalParity(root string) []Finding {
	return checkRefusalParity(root, inboxFamily, inboxWriters)
}

// checkSessionLeaseRefusalParity holds the ACP session methods to the lease
// family: a sentinel added beside these two must be named here before it can
// reach a client as prose.
func checkSessionLeaseRefusalParity(root string) []Finding {
	return checkRefusalParity(root, leaseFamily, sessionLeaseWriters)
}

// checkRefusalParity holds each frontend's boundary to one store family. A
// sentinel with no case of its own is answered as an internal fault — three were
// on HTTP, and every one of them was on ACP — and one whose case writes prose
// reaches the client as English nobody translated. Both are the same defect: a
// name lost between the kernel and the client.
func checkRefusalParity(root string, family refusalFamily, writers []refusalWriter) []Finding {
	sentinels, err := refusalSentinels(root, family)
	if err != nil {
		return []Finding{{family.file, 1, ruleRefusalPath, err.Error(), 1}}
	}
	var out []Finding
	for _, w := range writers {
		coded, uncoded, line, err := refusalHandledSentinels(root, family, w)
		if err != nil {
			out = append(out, Finding{w.file, 1, ruleRefusalPath, err.Error(), 1})
			continue
		}
		for _, name := range sentinels {
			switch {
			case coded[name]:
			case uncoded[name]:
				out = append(out, Finding{w.file, line, ruleRefusalPath, fmt.Sprintf(
					"%s is refused without a code in %s: the frontend gets an English sentence it cannot translate or branch on", name, w.fn), 1})
			default:
				out = append(out, Finding{w.file, line, ruleRefusalPath, fmt.Sprintf(
					"%s has no case in %s, so a known refusal is answered as an internal fault", name, w.fn), 1})
			}
		}
	}
	return out
}

// refusalSentinels reads the family from its declaration. A hand-kept list here
// would be the thing that rots: the sentinel added next is exactly the one
// nobody remembers to add twice.
func refusalSentinels(root string, family refusalFamily) ([]string, error) {
	file, _, err := parseRepoFile(root, family.file)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "Err") || i >= len(spec.Values) {
				continue
			}
			if call, ok := spec.Values[i].(*ast.CallExpr); ok && isSelector(call.Fun, "errors", "New") {
				out = append(out, name.Name)
			}
		}
		return true
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("no errors.New sentinels found in %s: this check has lost its subject", family.file)
	}
	return out, nil
}

// refusalHandledSentinels splits the writer's cases by what their body writes.
// Which sentinel a case is about comes from the selector it names, never from
// the code string beside it — the two can disagree, and then the guard would be
// reading the wrong one of them.
func refusalHandledSentinels(root string, family refusalFamily, w refusalWriter) (coded, uncoded map[string]bool, line int, err error) {
	file, fset, err := parseRepoFile(root, w.file)
	if err != nil {
		return nil, nil, 0, err
	}
	coded, uncoded = map[string]bool{}, map[string]bool{}
	found := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != w.fn {
			continue
		}
		found = true
		line = fset.Position(fn.Pos()).Line
		ast.Inspect(fn, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			named := caseSentinels(clause.List, family.pkg)
			writes := writesACode(clause.Body, w.coded)
			for _, name := range named {
				if writes {
					coded[name] = true
				} else {
					uncoded[name] = true
				}
			}
			return true
		})
	}
	if !found {
		return nil, nil, 0, fmt.Errorf("%s no longer declares %s: this check has lost its subject", w.file, w.fn)
	}
	return coded, uncoded, line, nil
}

// caseSentinels names the family errors a case matches on.
func caseSentinels(list []ast.Expr, pkg string) []string {
	var out []string
	for _, expr := range list {
		ast.Inspect(expr, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg && strings.HasPrefix(sel.Sel.Name, "Err") {
				out = append(out, sel.Sel.Name)
			}
			return true
		})
	}
	return out
}

// writesACode reports whether the branch answers with an identity. The code has
// to be a literal at the call: that is also what the frontend's own catalogue
// guard reads, so a code assembled at runtime is one neither side can check.
func writesACode(body []ast.Stmt, coded map[string]bool) bool {
	writes := false
	for _, stmt := range body {
		ast.Inspect(stmt, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || !coded[id.Name] {
				return true
			}
			for _, arg := range call.Args {
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.Contains(lit.Value, ".") {
					writes = true
				}
			}
			return true
		})
	}
	return writes
}

func parseRepoFile(root, rel string) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
	if err != nil {
		return nil, nil, err
	}
	return file, fset, nil
}
