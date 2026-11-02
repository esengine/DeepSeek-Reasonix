package team

import (
	"os"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/teamroster"
)

type unavailableCredentials struct{}

func (unavailableCredentials) Save(string) (string, error) {
	return "", teamroster.ErrSecretCredentials
}
func (unavailableCredentials) Read(string) (string, error) {
	return "", teamroster.ErrSecretCredentials
}

func TestFailedCredentialMigrationStillMasksLegacyValues(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	store := teamroster.Store{Dir: t.TempDir(), Credentials: unavailableCredentials{}}
	const value = "migration-private-0123456789"
	data := []byte(`{"secrets":[{"name":"MIGRATION","value":"` + value + `"}]}`)
	if err := os.WriteFile(store.SecretPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	WatchSecrets(store)
	if secrets.Masker().String(value) == value {
		t.Fatal("failed migration left a known secret unmasked")
	}
	if got, err := os.ReadFile(store.SecretPath()); err != nil || string(got) != string(data) {
		t.Fatal("failed migration changed legacy records")
	}
}

func TestActiveAndRetiredSecretsPersistOnlySlotsAndSurviveRestart(t *testing.T) {
	h := newHarness(t, "Chief")
	for _, value := range []string{"retired-private-001", "retired-private-002"} {
		if err := h.svc.UserSaveSecret(t.Context(), "ROTATION", value, "", []string{h.id("Chief")}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.svc.UserDeleteSecret(t.Context(), "ROTATION"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(h.store.SecretPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "retired-private") || strings.Contains(string(data), `"value"`) {
		t.Fatal("retired values remained in the team file")
	}
	reopened := teamroster.Store{Dir: h.store.Dir, Credentials: h.store.Credentials}
	book, err := reopened.Secrets()
	if err != nil || len(book.Retired) != 2 {
		t.Fatalf("retired restart = %+v, %v", book, err)
	}
	WatchSecrets(reopened)
	if secrets.Masker().String("retired-private-001 retired-private-002") != "[secret:ROTATION] [secret:ROTATION]" {
		t.Fatal("retired slots did not mask after restart")
	}
}

func TestSavingASecretRequiresExplicitRecipients(t *testing.T) {
	h := newHarness(t, "Chief")
	if err := h.svc.UserSaveSecret(t.Context(), "TOKEN", "private-123456789", "", nil, ""); err == nil {
		t.Fatal("missing recipients granted every agent")
	}
}

func TestForgetRetiredRemovesOnlyRetiredCredentialSlots(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	h := newHarness(t, "Chief")
	h.store.Credentials = secretCredentials{}
	h.svc = New(Options{Store: h.store, Panes: h.panes, Now: h.clock.now})
	for _, value := range []string{"forget-private-001", "forget-private-002"} {
		if err := h.svc.UserSaveSecret(t.Context(), "FORGET", value, "", []string{h.id("Chief")}, ""); err != nil {
			t.Fatal(err)
		}
	}
	book, err := h.store.SecretMetadata()
	if err != nil || len(book.Retired) != 1 || len(book.Secrets) != 1 {
		t.Fatalf("rotation metadata: %v", err)
	}
	WatchSecrets(h.store)
	if secrets.Masker().String("forget-private-001") == "forget-private-001" {
		t.Fatal("retired value was not registered")
	}
	if err := h.svc.UserForgetRetired(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := config.ReadHostCredential(book.Retired[0].Slot); ok {
		t.Fatal("forgotten slot remains in credential file")
	}
	if value, ok := config.ReadHostCredential(book.Secrets[0].Slot); !ok || value != "forget-private-002" {
		t.Fatal("forgetting retired values changed the active credential")
	}
	after, err := h.store.SecretMetadata()
	if err != nil || len(after.Retired) != 0 {
		t.Fatal("retired metadata remains")
	}
	if secrets.Masker().String("forget-private-001") == "forget-private-001" {
		t.Fatal("running process forgot a value already handed out")
	}
}
