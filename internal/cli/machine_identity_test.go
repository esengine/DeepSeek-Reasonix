package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func installMachineTestIdentity(t *testing.T) []byte {
	t.Helper()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", root)
	t.Setenv("REASONIX_STATE_HOME", "")
	key := bytes.Repeat([]byte{0x5a}, machineIdentityKeyBytes)
	if err := os.WriteFile(filepath.Join(root, machineIdentityKeyFile), key, 0o600); err != nil {
		t.Fatalf("write machine identity key: %v", err)
	}
	return key
}

func TestMachineIdentityKeyInitializesOnceAcrossConcurrentReaders(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", root)
	t.Setenv("REASONIX_STATE_HOME", "")

	type result struct {
		key []byte
		err error
	}
	const readers = 16
	results := make(chan result, readers)
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := loadMachineIdentityKey()
			results <- result{key: key, err: err}
		}()
	}
	wg.Wait()
	close(results)

	var want []byte
	for result := range results {
		if result.err != nil {
			t.Fatalf("load machine identity key: %v", result.err)
		}
		if want == nil {
			want = result.key
			continue
		}
		if !bytes.Equal(result.key, want) {
			t.Fatalf("concurrent readers observed different identity keys")
		}
	}
	if len(want) != machineIdentityKeyBytes {
		t.Fatalf("identity key length = %d, want %d", len(want), machineIdentityKeyBytes)
	}
	info, err := os.Stat(filepath.Join(root, machineIdentityKeyFile))
	if err != nil {
		t.Fatalf("stat machine identity key: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("identity key permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestMachineIdentityKeyCorruptionFailsClosed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", root)
	t.Setenv("REASONIX_STATE_HOME", "")
	path := filepath.Join(root, machineIdentityKeyFile)
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMachineIdentityKey(); err == nil {
		t.Fatal("corrupt identity key was silently accepted or rotated")
	}

	var out bytes.Buffer
	if code := runSessionCommand([]string{"list", "--json", "--dir", t.TempDir()}, &out); code != 1 {
		t.Fatalf("exit code = %d, output = %s", code, out.String())
	}
	var response machineErrorResponse
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode machine error: %v", err)
	}
	if response.Error.Code != "machine_identity_unavailable" || strings.Contains(out.String(), root) {
		t.Fatalf("machine error leaked identity details: %+v", response)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "corrupt" {
		t.Fatalf("corrupt identity key was silently replaced: %q", body)
	}
}

func TestEventsJSONLRejectsCorruptIdentityBeforeRuntimeSetup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", root)
	t.Setenv("REASONIX_STATE_HOME", "")
	if err := os.WriteFile(filepath.Join(root, machineIdentityKeyFile), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	var code int
	stderr := captureStderr(t, func() {
		code = runAgent([]string{"--events-jsonl", "do not start a provider run"})
	})
	if code != 1 || !strings.Contains(stderr, "machine identity is unavailable") {
		t.Fatalf("run exit=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stderr, root) {
		t.Fatalf("run error leaked identity path: %q", stderr)
	}
}
