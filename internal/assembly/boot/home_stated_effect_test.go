package boot

// A stated home is an input to Build, not something the process is mutated
// into: which config.toml and which credentials .env the assembly read, and
// that the environment's home stays untouched while a stated one is in force.

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// statedHomeWorkspace writes a project naming one provider kind whose key comes
// from keyEnv, and returns the workspace root.
func statedHomeWorkspace(t *testing.T, kind, keyEnv string) string {
	t.Helper()
	ws := robustTempDir(t)
	writeFile(t, ws, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
base_url = "http://127.0.0.1:1/v1"
api_key_env = "`+keyEnv+`"
`)
	return ws
}

// statedHome writes a Reasonix home holding a user-global system prompt and the
// credential the workspace's provider requires. credentials_store is set in the
// file rather than the environment so a caller can bind a home without touching
// the process at all.
func statedHome(t *testing.T, prompt, keyEnv, key string) string {
	t.Helper()
	home := robustTempDir(t)
	writeFile(t, home, "config.toml", fmt.Sprintf("credentials_store = \"file\"\n\n[agent]\nsystem_prompt = %q\n", prompt))
	if key != "" {
		writeFile(t, home, ".env", keyEnv+"="+key+"\n")
	}
	return home
}

// systemPromptFromBuild runs one turn through the real Build stack and returns
// the system message that actually reached the provider.
func systemPromptFromBuild(t *testing.T, home, ws, kind string) string {
	t.Helper()
	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	if _, err := config.RootsForHome(home).ApproveWorkspacePrograms(ws); err != nil {
		t.Fatal(err)
	}

	ctrl, err := Build(context.Background(), Options{
		Sink:          event.Discard,
		Home:          home,
		WorkspaceRoot: ws,
		RequireKey:    true,
	})
	if err != nil {
		t.Fatalf("Build with stated home %s: %v", home, err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, req := range rec.requests() {
		for _, m := range req.Messages {
			if m.Role == provider.RoleSystem {
				return m.Content
			}
		}
	}
	t.Fatal("no system message reached the provider boundary")
	return ""
}

// TestStatedHomeOutranksEnvironment: the environment is how a host works out
// which home to use, not a second opinion once it has said so.
func TestStatedHomeOutranksEnvironment(t *testing.T) {
	fenceBootTestHistoryCatalog(t)
	const keyEnv = "REASONIX_STATED_HOME_KEY"
	decoy := robustTempDir(t)
	writeFile(t, decoy, "config.toml", "credentials_store = \"file\"\n\n[agent]\nsystem_prompt = \"FROM-ENVIRONMENT\"\n")
	writeFile(t, decoy, ".env", keyEnv+"=decoy\n")
	t.Setenv("REASONIX_HOME", decoy)

	stated := statedHome(t, "FROM-STATED-HOME", keyEnv, "stated")
	ws := statedHomeWorkspace(t, "stated-home-effect", keyEnv)

	got := systemPromptFromBuild(t, stated, ws, "stated-home-effect")
	if !strings.Contains(got, "FROM-STATED-HOME") {
		t.Fatalf("system prompt came from the wrong home: %q", firstStatedHomeLine(got))
	}
	if want := filepath.Join(stated, ".env"); config.RootsForHome(stated).UserCredentialsPath() != want {
		t.Fatalf("credentials path = %q, want %q", config.RootsForHome(stated).UserCredentialsPath(), want)
	}
}

// TestStatedHomeLeavesEnvironmentHomeUntouched: an assembly bound to one home
// must not write another, or "one process, two installs" is a claim about
// resolution rather than isolation. The exception is named, not excluded
// quietly: the history-search projection is one per-process SQLite file under
// the process cache root, so a stated home does not yet get its own.
func TestStatedHomeLeavesEnvironmentHomeUntouched(t *testing.T) {
	fenceBootTestHistoryCatalog(t)
	const keyEnv = "REASONIX_UNTOUCHED_HOME_KEY"
	untouched := robustTempDir(t)
	t.Setenv("REASONIX_HOME", untouched)

	stated := statedHome(t, "STATED", keyEnv, "stated")
	ws := statedHomeWorkspace(t, "untouched-home-effect", keyEnv)
	systemPromptFromBuild(t, stated, ws, "untouched-home-effect")

	var leaked []string
	err := filepath.WalkDir(untouched, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == untouched {
			return err
		}
		rel, relErr := filepath.Rel(untouched, path)
		if relErr != nil {
			return relErr
		}
		if strings.HasPrefix(filepath.ToSlash(rel), "cache/") {
			return nil // process-wide derived projection, not this home's state
		}
		leaked = append(leaked, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk environment home: %v", err)
	}
	if len(leaked) != 0 {
		t.Fatalf("environment home was written while a home was stated: %v", leaked)
	}
}

// TestStatedHomesCoexistInOneProcess is the payoff: two assemblies, two homes,
// one process. Nothing in this test calls t.Setenv or t.Chdir.
func TestStatedHomesCoexistInOneProcess(t *testing.T) {
	fenceBootTestHistoryCatalog(t)
	const keyEnv = "REASONIX_COEXIST_HOME_KEY"
	for _, name := range []string{"ALPHA", "BETA"} {
		home := statedHome(t, name, keyEnv, "k")
		ws := statedHomeWorkspace(t, "coexist-"+strings.ToLower(name), keyEnv)
		got := systemPromptFromBuild(t, home, ws, "coexist-"+strings.ToLower(name))
		if !strings.Contains(got, name) {
			t.Fatalf("assembly for %s read another home's prompt: %q", name, firstStatedHomeLine(got))
		}
	}
}

func firstStatedHomeLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
