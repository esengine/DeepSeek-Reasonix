package secrets

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestSinkGuardRecognizesNewFormattingSinks(t *testing.T) {
	for _, tc := range []struct {
		expression string
		unsafe     bool
	}{
		{`entry.URL`, true},
		{`strings.TrimSpace(entry.URL)`, true},
		{`entry.Headers["Authorization"]`, true},
		{`entry.Env["TOKEN"]`, true},
		{`secrets.RedactEndpoint(entry.URL)`, false},
		{`secrets.RedactConfigValue(key, entry.Env[key])`, false},
		{`secrets.RedactConfigMap(entry.Headers)`, false},
		{`entry.Name`, false},
	} {
		expression, err := parser.ParseExpr(tc.expression)
		if err != nil {
			t.Fatal(err)
		}
		if got := rawCredentialField(expression); got != tc.unsafe {
			t.Errorf("sink %s unsafe=%t, want %t", tc.expression, got, tc.unsafe)
		}
	}
}

func TestSinkGuardTracksLocalAliases(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "neutral.go", `package neutral
func display(entry Entry) {
 value := entry.URL
 alias := strings.TrimSpace(value)
 fmt.Printf("%s", alias)
 for _, header := range entry.Headers { fmt.Printf("%s", header) }
}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	body := file.Decls[0].(*ast.FuncDecl).Body
	sources := sinkSources(body)
	for _, name := range []string{"value", "alias", "header"} {
		if !credentialSource(ast.NewIdent(name), sources) {
			t.Errorf("unprojected alias escaped: %s", name)
		}
	}
}
