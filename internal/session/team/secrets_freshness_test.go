package team

import (
	"os"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/teamroster"
)

func TestSecretWatcherSeesSameSizeRewriteWithRestoredMtime(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	store := teamroster.Store{Dir: t.TempDir(), Credentials: &testSecretCredentials{values: map[string]string{}}}
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(store.SecretPath(), []byte(`{"secrets":[{"name":"REWRITE","value":"`+value+`"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const first = "same-size-secret-001"
	const second = "same-size-secret-002"
	write(first)
	WatchSecrets(store)
	if secrets.Masker().String(first) == first {
		t.Fatal("initial value unmasked")
	}
	info, err := os.Stat(store.SecretPath())
	if err != nil {
		t.Fatal(err)
	}
	write(second)
	if err := os.Chtimes(store.SecretPath(), info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	m := secrets.Masker()
	if m.String(second) == second || m.String(first) == first {
		t.Fatal("rewritten or retired value unmasked")
	}
}

func TestSecretWatcherSeesCredentialRewriteWithoutMetadataChange(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	const first = "credential-private-001"
	const second = "credential-private-002"
	slot, err := config.SaveHostCredential(first)
	if err != nil {
		t.Fatal(err)
	}
	store := WithSecretCredentials(teamroster.Store{Dir: t.TempDir()})
	if err := os.WriteFile(store.SecretPath(), []byte(`{"secrets":[{"name":"CREDENTIAL_REWRITE","slot":"`+slot+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	WatchSecrets(store)
	if secrets.Masker().String(first) == first {
		t.Fatal("initial credential unmasked")
	}
	path := config.UserCredentialsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), first, second)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	m := secrets.Masker()
	if m.String(first) == first || m.String(second) == second {
		t.Fatal("credential rewrite bypassed masking or forgot the previous value")
	}
}
