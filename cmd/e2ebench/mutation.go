package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxMutants = 15

type mutationResult struct {
	caught, total int
	survivors     []string
}

// runMutation gives a behavioral signal that the differential can't for additive
// PRs: it replaces each changed function's body with a zero-value return (which
// compiles for any signature via *new(T)), runs only the agent's new tests for
// that package, and records whether they catch the mutation. A caught mutant
// means a test actually asserts that function's output; a survivor means the new
// tests don't check it.
func runMutation(repo, base string, srcFiles []string, refs []testRef) mutationResult {
	changed := changedLineSet(repo, base, srcFiles)
	byPkg := map[string][]string{}
	for _, r := range refs {
		byPkg[r.pkg] = append(byPkg[r.pkg], r.name)
	}

	var res mutationResult
	for _, file := range srcFiles {
		if res.total >= maxMutants {
			break
		}
		pkg := "./" + filepath.ToSlash(filepath.Dir(file))
		tests := byPkg[pkg]
		if len(tests) == 0 {
			continue // no new tests to attribute a catch to
		}
		runRe := "^(" + strings.Join(tests, "|") + ")$"

		abs := filepath.Join(repo, filepath.FromSlash(file))
		srcB, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, abs, srcB, 0)
		if err != nil {
			continue
		}
		src := string(srcB)
		for _, fd := range changedFuncs(fset, f, changed[file]) {
			if res.total >= maxMutants {
				break
			}
			res.total++
			lb := fset.Position(fd.Body.Lbrace).Offset
			rb := fset.Position(fd.Body.Rbrace).Offset
			mutated := src[:lb] + mutantBody(fset, fd.Type.Results) + src[rb+1:]
			if os.WriteFile(abs, []byte(mutated), 0o644) != nil {
				res.total--
				continue
			}
			cmd := exec.Command("go", "test", "-run", runRe, pkg)
			cmd.Dir = repo
			cmd.WaitDelay = 2 * time.Minute // bound the wait for a mutant that wedges a test
			// Restore source even on panic; a file left mutated would corrupt the next mutant.
			restored := false
			defer func() {
				if !restored {
					_ = os.WriteFile(abs, srcB, 0o644)
				}
			}()
			caught := cmd.Run() != nil
			_ = os.WriteFile(abs, srcB, 0o644)
			restored = true
			if caught {
				res.caught++
			} else {
				res.survivors = append(res.survivors, fd.Name.Name)
			}
		}
	}
	return res
}

// changedFuncs returns the funcs in f whose line range overlaps a changed line.
// main/init and nil-named decls are skipped (no meaningful return to mutate).
func changedFuncs(fset *token.FileSet, f *ast.File, lines map[int]bool) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil || fd.Name == nil {
			continue
		}
		name := fd.Name.Name
		if name == "main" || name == "init" {
			continue
		}
		start := fset.Position(fd.Pos()).Line
		end := fset.Position(fd.End()).Line
		for ln := range lines {
			if ln >= start && ln <= end {
				out = append(out, fd)
				break
			}
		}
	}
	return out
}

// mutantBody returns a replacement body that returns the zero value for each
// result. *new(T) is the zero value of any type T, so this compiles for every
// signature without naming the results or knowing their concrete types.
func mutantBody(fset *token.FileSet, results *ast.FieldList) string {
	if results == nil || len(results.List) == 0 {
		return "{\n}"
	}
	var rets []string
	for _, field := range results.List {
		t := "*new(" + printType(fset, field.Type) + ")"
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			rets = append(rets, t)
		}
	}
	return "{\n\treturn " + strings.Join(rets, ", ") + "\n}"
}

func printType(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	_ = printer.Fprint(&b, fset, e)
	return b.String()
}
