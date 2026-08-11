package gen

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGeneratedArtifactIsCurrent pins the committed companion TypeScript to the
// schema: if schema.json changes without regenerating, this test fails. The
// companion side has the same guard in its own unit test.
func TestGeneratedArtifactIsCurrent(t *testing.T) {
	generated, err := GenerateTypeScript()
	if err != nil {
		t.Fatalf("GenerateTypeScript: %v", err)
	}
	path := filepath.Join("..", "..", "..", "browser-companion", "src", "generated", "browserProtocol.generated.ts")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run cmd/browser-ipc-gen)", path, err)
	}
	if string(current) != string(generated) {
		t.Fatalf("%s is stale; run cmd/browser-ipc-gen from the desktop module root", path)
	}
}
