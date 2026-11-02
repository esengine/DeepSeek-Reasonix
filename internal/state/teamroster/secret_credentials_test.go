package teamroster

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

type testSecretCredentials map[string]string

func (c testSecretCredentials) Save(value string) (string, error) {
	slot := fmt.Sprint(len(c) + 1)
	c[slot] = value
	return slot, nil
}

type failingSecretCredentials struct{}

func (failingSecretCredentials) Save(string) (string, error) { return "", ErrSecretCredentials }
func (failingSecretCredentials) Read(string) (string, error) { return "", ErrSecretCredentials }

func TestLegacySecretMigrationPreservesTheFileUntilCredentialsAreDurable(t *testing.T) {
	s := Store{Dir: t.TempDir(), Credentials: failingSecretCredentials{}}
	legacy := []byte(`{"secrets":[{"name":"SHOP","value":"legacy-0123456789"}],"requests":[]}`)
	if err := os.WriteFile(s.SecretPath(), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.EditSecrets(t.Context(), func(*SecretBook) error { return nil }); !errors.Is(err, ErrSecretCredentials) {
		t.Fatalf("migration = %v", err)
	}
	got, _ := os.ReadFile(s.SecretPath())
	if string(got) != string(legacy) {
		t.Fatal("failed migration modified original values")
	}
	s.Credentials = testSecretCredentials{}
	if err := s.EditSecrets(t.Context(), func(*SecretBook) error { return nil }); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(s.SecretPath())
	if strings.Contains(string(got), "legacy-0123456789") {
		t.Fatal("successful migration retained plaintext")
	}
	book, err := s.Secrets()
	if err != nil || book.Secrets[0].Value != "legacy-0123456789" {
		t.Fatalf("migrated value unavailable: %v", err)
	}
}
func (c testSecretCredentials) Read(slot string) (string, error) {
	value, ok := c[slot]
	if !ok {
		return "", ErrSecretCredentials
	}
	return value, nil
}

func TestSecretValuesLeaveOnlyCredentialReferencesInTeamFiles(t *testing.T) {
	s := Store{Dir: t.TempDir(), Credentials: testSecretCredentials{}}
	const value = "private-value-0123456789"
	if err := s.EditSecrets(t.Context(), func(b *SecretBook) error {
		b.Secrets = append(b.Secrets, Secret{Name: "SHOP", Value: value})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.SecretPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), value) || strings.Contains(string(data), `"value"`) {
		t.Fatalf("plaintext remains in metadata: %s", data)
	}
	reopened := Store{Dir: s.Dir, Credentials: s.Credentials}
	book, err := reopened.Secrets()
	if err != nil || len(book.Secrets) != 1 || book.Secrets[0].Value != value {
		t.Fatalf("credential restart read failed: %v", err)
	}
}
