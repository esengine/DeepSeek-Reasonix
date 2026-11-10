package sessionstore

import (
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"testing"
)

func TestCheckpointReadersConfineSymlinksToSessionDirectory(t *testing.T) {
	parent := testenv.TempDir(t)
	dir := filepath.Join(parent, "sessions")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"outside.jsonl", "sessions/inside.jsonl"} {
		if err := os.WriteFile(filepath.Join(parent, filepath.FromSlash(name)), []byte("{\"role\":\"user\",\"content\":\"saved\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, target string
		allowed      bool
	}{
		{"inside", "inside.jsonl", true},
		{"outside", filepath.Join("..", "outside.jsonl"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+"-link.jsonl")
			if err := os.Symlink(tc.target, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			msgs, err := loadSessionMessagesFromJSONLWithLimits(path, defaultSessionReplayLimits)
			tail := loadSessionMessagesFromJSONLKeepingDecodedTailWithLimits(path, defaultSessionReplayLimits)
			if tc.allowed {
				if err != nil || len(msgs) != 1 || len(tail) != 1 || msgs[0].Content != "saved" || tail[0].Content != "saved" {
					t.Fatalf("same-directory checkpoint: messages=%v tail=%v error=%v", msgs, tail, err)
				}
			} else if err == nil || len(msgs) != 0 || len(tail) != 0 {
				t.Fatalf("checkpoint escaped directory: messages=%v tail=%v error=%v", msgs, tail, err)
			}
		})
	}
}
