package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCLICompletionCoversRunDispatch(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "cli.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var dispatch *ast.SwitchStmt
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Run" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switchStatement, ok := node.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			identifier, ok := switchStatement.Tag.(*ast.Ident)
			if ok && identifier.Name == "cmd" {
				dispatch = switchStatement
				return false
			}
			return true
		})
	}
	if dispatch == nil {
		t.Fatal("Run cmd dispatch switch not found")
	}

	root := cliCompletionRootSpec()
	for _, statement := range dispatch.Body.List {
		clause, ok := statement.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range clause.List {
			literal, ok := expression.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			command, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !completionRegistryKnowsRootToken(&root, command) {
				t.Errorf("Run dispatch command %q is missing from the shell completion registry", command)
			}
		}
	}
}

func TestCLICompletionListsRootAndNestedCommands(t *testing.T) {
	root := cliCompletionRootSpec()
	values := func(cliCompletionValueKind) []string { return nil }

	got := cliCompletionCandidatesWithValues(root, 1, []string{"reasonix", "co"}, values)
	if want := []string{"config", "completion"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root completion = %v, want %v", got, want)
	}

	got = cliCompletionCandidatesWithValues(root, 2, []string{"reasonix", "mcp", "b"}, values)
	if want := []string{"browse"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mcp subcommand completion = %v, want %v", got, want)
	}

	got = cliCompletionCandidatesWithValues(root, 3, []string{"reasonix", "remote", "serve", "st"}, values)
	if want := []string{"start", "status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("remote serve completion = %v, want %v", got, want)
	}
}

func TestCLICompletionListsCommandFlags(t *testing.T) {
	root := cliCompletionRootSpec()
	values := func(cliCompletionValueKind) []string { return nil }

	got := cliCompletionCandidatesWithValues(root, 2, []string{"reasonix", "run", "--m"}, values)
	for _, want := range []string{"--model", "--max-steps", "--metrics"} {
		if !containsCompletionValue(got, want) {
			t.Errorf("run flag completion missing %q: %v", want, got)
		}
	}

	got = cliCompletionCandidatesWithValues(root, 1, []string{"reasonix", "--d"}, values)
	for _, want := range []string{"--dir", "--dangerously-skip-permissions"} {
		if !containsCompletionValue(got, want) {
			t.Errorf("root flag completion missing %q: %v", want, got)
		}
	}

	got = cliCompletionCandidatesWithValues(root, 3, []string{"reasonix", "mcp", "add", "--h"}, values)
	for _, want := range []string{"--http", "--header", "--help"} {
		if !containsCompletionValue(got, want) {
			t.Errorf("mcp add flag completion missing %q: %v", want, got)
		}
	}
}

func TestCLICompletionUsesConfiguredModelsAndSessionIDs(t *testing.T) {
	root := cliCompletionRootSpec()
	values := func(kind cliCompletionValueKind) []string {
		switch kind {
		case cliCompletionModelValue:
			return []string{"deepseek/deepseek-chat", "mimo/mimo-v2"}
		case cliCompletionSessionValue:
			return []string{"alpha-session", "beta-session"}
		default:
			return nil
		}
	}

	got := cliCompletionCandidatesWithValues(root, 3, []string{"reasonix", "run", "--model", "deep"}, values)
	if want := []string{"deepseek/deepseek-chat"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("model completion = %v, want %v", got, want)
	}

	got = cliCompletionCandidatesWithValues(root, 1, []string{"reasonix", "--model=mi"}, values)
	if want := []string{"--model=mimo/mimo-v2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("inline model completion = %v, want %v", got, want)
	}

	got = cliCompletionCandidatesWithValues(root, 2, []string{"reasonix", "--resume", "b"}, values)
	if want := []string{"beta-session"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("session completion = %v, want %v", got, want)
	}
}

func TestCompletionCommandPrintsShellScripts(t *testing.T) {
	isolateCLIConfigHome(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			out := captureStdout(t, func() {
				if code := Run([]string{"completion", shell}, "test-version"); code != 0 {
					t.Fatalf("completion %s exit code = %d", shell, code)
				}
			})
			if !strings.Contains(out, "reasonix completion __complete") {
				t.Fatalf("completion %s script does not route to the shared registry:\n%s", shell, out)
			}
		})
	}
}

func containsCompletionValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func completionRegistryKnowsRootToken(root *cliCompletionSpec, token string) bool {
	if strings.HasPrefix(token, "-") {
		flag, _ := cliCompletionLookupFlag(root, token)
		return flag != nil
	}
	return cliCompletionLookupSubcommand(root, token) != nil
}
