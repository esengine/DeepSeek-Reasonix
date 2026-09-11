package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSessionPathKeepsUntrustedModelInOnePortableComponent(t *testing.T) {
	for _, model := range []string{"../../outside", `..\..\outside`, "vendor/model:tag", "model\x00suffix", "model\nline", "模型-v1.2"} {
		t.Run(model, func(t *testing.T) {
			dir := t.TempDir()
			path := NewSessionPath(dir, model)
			rel, err := filepath.Rel(dir, path)
			if err != nil || rel != filepath.Base(path) || !filepath.IsLocal(rel) {
				t.Fatalf("model escaped session directory: %q, %v", path, err)
			}
			if strings.ContainsAny(rel, "<>:\"/\\|?*\x00\n\r") {
				t.Fatalf("nonportable session filename: %q", rel)
			}
			if strings.ContainsAny(model, "\x00\n") && !strings.HasSuffix(rel, "-session.jsonl") {
				t.Fatalf("invalid label did not use the safe fallback: %q", rel)
			}
		})
	}
}
