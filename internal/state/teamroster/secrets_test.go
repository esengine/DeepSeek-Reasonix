package teamroster

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestASecretNameIsNormalisedIntoAnEnvironmentSuffix(t *testing.T) {
	for in, want := range map[string]string{"shop key": "SHOP_KEY", "github-token": "GITHUB_TOKEN", "  x  ": "X", "9lives": "", "": "", "密码": ""} {
		if got := NormalizeSecretName(in); got != want {
			t.Errorf("NormalizeSecretName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecretsAreKeptInAFileOnlyTheUserReads(t *testing.T) {
	s := Store{Dir: t.TempDir(), Credentials: testSecretCredentials{}}
	ctx := context.Background()
	if err := s.EditSecrets(ctx, func(b *SecretBook) error {
		b.Requests = append(b.Requests, SecretRequest{Name: "SHOP_KEY", Purpose: "the shop API", Agent: newAgentID()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.EditSecrets(ctx, func(b *SecretBook) error {
		b.Secrets = append(b.Secrets, Secret{Name: "SHOP_KEY", Value: "abcd-1234"})
		b.Requests = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	book, err := s.Secrets()
	if err != nil || len(book.Secrets) != 1 || book.Secrets[0].Value != "abcd-1234" || len(book.Requests) != 0 {
		t.Fatalf("Secrets = %+v, %v", book, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(s.SecretPath()); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("secrets file mode = %v, %v", info.Mode(), err)
		}
	}
	for name, bad := range map[string]Secret{
		"bad name":  {Name: "shop key", Value: "abcd-1234"},
		"too short": {Name: "PIN", Value: "123"},
		"bad agent": {Name: "X", Value: "abcd-1234", Agents: []string{"nobody"}},
	} {
		if err := s.EditSecrets(ctx, func(b *SecretBook) error { b.Secrets = append(b.Secrets, bad); return nil }); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrTooLong) && !errors.Is(err, ErrEmpty) {
			t.Errorf("%s: EditSecrets = %v", name, err)
		}
	}
}
