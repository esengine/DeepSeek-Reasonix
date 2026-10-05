package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestLoadCompactionStateReadsOtherReleaseSchemas(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		unsupported bool
	}{
		{"1.x schema 4", `{"schema_version":4,"projection":{"messages":[{"role":"user","content":"s"}],"covered_count":1,"covered_prefix_hash":"h"}}`, false},
		{"1.x schema 4 with a pinned checkpoint", `{"schema_version":4,"projection":{"messages":[{"role":"user","content":"s"}],"covered_count":1,"covered_prefix_hash":"h","pinned_context_hash":"sha256:ab"}}`, true},
		{"unknown schema", `{"schema_version":99,"projection":{"messages":[]}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(testenv.TempDir(t), "s.jsonl")
			if err := os.WriteFile(ContextStatePath(path), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			st, ok, err := LoadCompactionState(path)
			if tc.unsupported {
				if !errors.Is(err, ErrContextSchemaUnsupported) || ok {
					t.Fatalf("load = ok %v, err %v; want ErrContextSchemaUnsupported", ok, err)
				}
				return
			}
			if err != nil || !ok || !st.Foreign() || st.Projection.CoveredPrefixHash != "h" {
				t.Fatalf("load = %+v, ok %v, err %v", st, ok, err)
			}
		})
	}
}
