package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func writeSecret(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(testenv.TempDir(t), "serve.password")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// --hash-password reads --password-file, so producing the stored hash never
// puts the password in argv either.
func TestHashPasswordReadsThePasswordFile(t *testing.T) {
	p := writeSecret(t, "MySecret123\n", 0o600)
	var code int
	var out string
	captureStderr(t, func() {
		out = captureStdout(t, func() {
			code = runServeWithOptions([]string{"--hash-password", "--password-file", p}, serveRunOptions{command: "serve"})
		})
	})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(strings.TrimSpace(out)), []byte("MySecret123")); err != nil {
		t.Fatalf("printed hash does not match the file's password: %v", err)
	}
}

// A password file stands in for --password at startup, wins over it, and is
// held to the same privacy rule as a token file.
func TestServePasswordFileSetsThePasswordHash(t *testing.T) {
	p := writeSecret(t, "MySecret123\n", 0o600)
	cfg, code := serveConfigFromFlags("serve", true, config.ServeConfig{}, serveFlagValues{
		auth: "password", password: "from-argv", passwordFile: p,
	})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cfg.PasswordHash), []byte("MySecret123")); err != nil {
		t.Fatalf("hash is not the file's password: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	loose := writeSecret(t, "MySecret123\n", 0o644)
	stderr := captureStderr(t, func() {
		_, code = serveConfigFromFlags("serve", true, config.ServeConfig{}, serveFlagValues{auth: "password", passwordFile: loose})
	})
	if code == 0 || !strings.Contains(stderr, "chmod 600") {
		t.Fatalf("a group-readable password file was accepted: code=%d stderr=%q", code, stderr)
	}
}
