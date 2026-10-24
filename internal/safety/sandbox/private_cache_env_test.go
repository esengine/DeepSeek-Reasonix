package sandbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateProcessEnvKeepsExplicitChoicesAndPrivateDefaults(t *testing.T) {
	root := t.TempDir()
	spec := Spec{RequiredIntegrity: CompleteFileIntegrity, ShellEnv: map[string]string{"GOCACHE": "declared-cache"}}
	values := PrivateProcessEnv(spec, root, false)
	seen := map[string]string{}
	for _, value := range values {
		key, entry, _ := strings.Cut(value, "=")
		seen[key] = entry
	}
	if _, changed := seen["GOCACHE"]; changed {
		t.Fatal("explicit cache choice replaced")
	}
	if seen["GOMODCACHE"] != filepath.Join(root, "cache", "go-mod") || seen["TMPDIR"] != root {
		t.Fatalf("cache escaped private temporary root: %v", seen)
	}
	if got := PrivateProcessEnv(Spec{}, root, false); len(got) != len(SessionTempEnvKeys) {
		t.Fatalf("legacy environment changed: %v", got)
	}
}
