package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

func TestCarriedProofInvalidatesPermissionChange(t *testing.T) {
	a, path := provedAgent(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(path, before.Mode().Perm()) }()
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || before.Mode() == after.Mode() {
		t.Fatalf("fixture did not change permissions: before=%v after=%v err=%v", before.Mode(), after, err)
	}
	if a.carriedProof() != nil {
		t.Fatal("permission-only change retained old proof")
	}
	if got := a.finalReadinessCheckFor(); got.missingVerification == 0 || got.missingSignoff == 0 || got.missingPathInspection == 0 {
		t.Fatalf("permission change was not sent back for proof: %+v", got)
	}
}

func TestCarriedProofFileStateCompatibility(t *testing.T) {
	for _, kind := range []string{"legacy_file", "legacy_absent", "future", "current_file", "current_absent"} {
		t.Run(kind, func(t *testing.T) {
			a, path := provedAgent(t)
			cp := a.DeliveryCheckpoint()
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(content)
			legacy := hex.EncodeToString(sum[:])
			if strings.HasSuffix(kind, "absent") {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				legacy = "absent"
			}
			want := a.pathFingerprint(path)
			switch kind {
			case "legacy_file", "legacy_absent":
				want = legacy
			case "future":
				want = "file-state-v99:unknown"
			}
			cp.Proven.Paths = map[string]string{"auth/login.go": want}
			raw, err := json.Marshal(cp)
			if err != nil {
				t.Fatal(err)
			}
			var loaded evidence.DeliveryCheckpoint
			if err := json.Unmarshal(raw, &loaded); err != nil {
				t.Fatal(err)
			}
			a.RestoreDeliveryCheckpoint(loaded)
			accept := strings.HasPrefix(kind, "current_")
			if (a.carriedProof() != nil) != accept {
				t.Fatalf("kind=%s carried proof accepted=%v", kind, !accept)
			}
			after, err := json.Marshal(a.DeliveryCheckpoint())
			if err != nil || string(after) != string(raw) {
				t.Fatal("checking proof rewrote the persisted checkpoint")
			}
			if accept && want == legacy {
				t.Fatal("previous byte-only reader could accept the new fingerprint")
			}
		})
	}
}

func TestCarriedProofRejectsDirectoryState(t *testing.T) {
	a, path := provedAgent(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if a.pathFingerprint(path) != "" || a.carriedProof() != nil {
		t.Fatal("directory acquired a file proof fingerprint")
	}
}

func BenchmarkCarriedProofFingerprint(b *testing.B) {
	for _, size := range []int{4096, 1 << 20} {
		name := "4KiB"
		if size > 4096 {
			name = "1MiB"
		}
		b.Run(name, func(b *testing.B) {
			path := filepath.Join(testenv.TempDir(b), "proof.bin")
			if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
				b.Fatal(err)
			}
			a := &Agent{}
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if a.pathFingerprint(path) == "" {
					b.Fatal("fingerprint unavailable")
				}
			}
		})
	}
}
