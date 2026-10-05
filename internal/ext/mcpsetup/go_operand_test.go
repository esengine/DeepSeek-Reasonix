package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestGoServerNameUsesRunPackage(t *testing.T) {
	for _, tc := range []struct {
		command string
		args    []string
		name    string
	}{
		{"go", []string{"run", "example.com/tools/reasonix-server@latest", "--stdio"}, "reasonix-server"},
		{"go", []string{"run", "example.com/tools/reasonix-server@v1.2.3"}, "reasonix-server"},
		{"/usr/local/go/bin/go", []string{"run", "./cmd/reasonix-server", "client-mode"}, "reasonix-server"},
		{"GO.EXE", []string{"run", "./cmd/reasonix-server"}, "reasonix-server"},
		{"go", []string{"-C", "workspace", "run", "./cmd/reasonix-server"}, "reasonix-server"},
		{"go", []string{"-C=workspace", "run", "./cmd/reasonix-server"}, "reasonix-server"},
		{"go", []string{"run", "-C", "workspace", "./cmd/reasonix-server"}, "reasonix-server"},
		{"go", []string{"run", "-race", "-trimpath", "./cmd/reasonix-server"}, "reasonix-server"},
		{"go", []string{"run", "--", "./cmd/reasonix-server", "-tags", "client-mode"}, "reasonix-server"},
		{"go", []string{"run", "./cmd/reasonix-server", "-exec", "client-mode"}, "reasonix-server"},
		{"go", []string{"run", "./reasonix_server.go"}, "reasonix-server-go"},
		{"go", []string{"run", "."}, "mcp-server"},
		{"go", []string{"run"}, "mcp-server"},
		{"go", []string{"run", "-tags"}, "mcp-server"},
		{"go", []string{"-C", "workspace"}, "mcp-server"},
		{"go", []string{"test", "./cmd/reasonix-server"}, "mcp-server"},
		{"go", nil, "mcp-server"},
		{"demo-server", []string{"run", "another-package"}, "demo-server"},
	} {
		t.Run(tc.command+" "+strings.Join(tc.args, " "), func(t *testing.T) {
			if got := NameFromArgv(tc.command, tc.args); got != tc.name {
				t.Errorf("name = %q, want %q", got, tc.name)
			}
			entry, err := ParseArgs(append([]string{"--", tc.command}, tc.args...))
			if err != nil || entry.Name != tc.name || entry.Command != tc.command || !reflect.DeepEqual(entry.Args, tc.args) {
				t.Errorf("CLI entry = %+v, err=%v", entry, err)
			}
			explicit, err := ParseArgs(append([]string{"manual", "--", tc.command}, tc.args...))
			if err != nil || explicit.Name != "manual" || !reflect.DeepEqual(explicit.Args, tc.args) {
				t.Errorf("explicit entry = %+v, err=%v", explicit, err)
			}
			draft, err := Parse(tc.command + " " + strings.Join(tc.args, " "))
			if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Name != tc.name || !reflect.DeepEqual(draft.Entries[0].Args, tc.args) {
				t.Errorf("pasted draft = %+v, err=%v", draft, err)
			}
		})
	}
}

func TestGoServerNameSkipsBuildFlagValues(t *testing.T) {
	for _, flag := range []string{
		"-C", "-p", "-covermode", "-coverpkg", "-asmflags", "-buildmode", "-compiler",
		"-gccgoflags", "-gcflags", "-installsuffix", "-ldflags", "-mod", "-modfile",
		"-overlay", "-pgo", "-pkgdir", "-tags", "-toolexec", "-exec",
	} {
		for _, args := range [][]string{
			{"run", flag, "build-value", "example.com/tools/reasonix-server@v1.2.3", "client-mode"},
			{"run", flag + "=build-value", "example.com/tools/reasonix-server@v1.2.3", "client-mode"},
			{"run", flag, "build-value"},
		} {
			want := "reasonix-server"
			if len(args) == 3 && args[2] == "build-value" {
				want = "mcp-server"
			}
			if got := NameFromArgv("go", args); got != want {
				t.Errorf("name for %v = %q, want %q", args, got, want)
			}
		}
	}
}

func TestGoServerNamePreservesQuotedBuildArguments(t *testing.T) {
	input := `go run -ldflags "-X main.mode=fixture" example.com/tools/reasonix-server@v1.2.3 --stdio`
	want := []string{"run", "-ldflags", "-X main.mode=fixture", "example.com/tools/reasonix-server@v1.2.3", "--stdio"}
	draft, err := Parse(input)
	if err != nil || len(draft.Entries) != 1 {
		t.Fatalf("pasted draft = %+v, err=%v", draft, err)
	}
	entry := draft.Entries[0]
	if entry.Name != "reasonix-server" || entry.Command != "go" || !reflect.DeepEqual(entry.Args, want) {
		t.Errorf("quoted entry = %+v", entry)
	}
	if len(draft.Risks) != 1 || draft.Risks[0].Server != entry.Name || draft.Risks[0].Kind != "shell" || draft.Risks[0].Detail != "go "+strings.Join(want, " ") {
		t.Errorf("quoted disclosure = %+v", draft.Risks)
	}
}
