package config

import (
	"os"
	"testing"
)

func TestHostCredentialIsNotExportedBySaveOrReload(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	const value = "private\nmultiline-value-0123456789"
	slot, err := SaveHostCredential(value)
	if err != nil {
		t.Fatal(err)
	}
	if !IsHostCredentialSlot(slot) {
		t.Fatal("invalid slot")
	}
	if _, ok := os.LookupEnv(slot); ok {
		t.Fatal("save exported a host credential")
	}
	loadDotEnvFileAs(UserCredentialsPath(), CredentialSource{Kind: CredentialSourceCredentials})
	if _, ok := os.LookupEnv(slot); ok {
		t.Fatal("reload exported a host credential")
	}
	t.Setenv(slot, "spoofed")
	if got, ok := ReadHostCredential(slot); !ok || got != value {
		t.Fatal("environment replaced stored host credential")
	}
}
