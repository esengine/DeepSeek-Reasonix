package secrets

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func credHome(t *testing.T, files ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, rel := range files {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestCredentialReadPath(t *testing.T) {
	home := credHome(t)
	for _, tc := range []struct {
		rel  string
		deny bool
	}{
		// Private keys take user-chosen names, so the ~/.ssh rule is written as
		// an exception list; "id_*" would miss every one of these but the first.
		{".ssh/id_ed25519", true},
		{".ssh/work_github", true},
		{".ssh/deploy", true},
		{".ssh/id_ed25519.pub", false},
		{".ssh/config", false},
		{".ssh/known_hosts", false},
		{".ssh/authorized_keys", false},
		{".aws/credentials", true},
		{".aws/config", false},
		{".netrc", true},
		{".git-credentials", true},
		{".pypirc", true},
		{".config/gcloud/application_default_credentials.json", true},
		// Mixed configuration+credential files stay readable: denying them
		// breaks the tool that owns them, which a broker must solve instead.
		{".config/gh/hosts.yml", false},
		{".npmrc", false},
		{".docker/config.json", false},
		{"projects/app/.env", false},
	} {
		if got := CredentialReadPath(filepath.Join(home, tc.rel)); got != tc.deny {
			t.Errorf("CredentialReadPath(%s) = %v, want %v", tc.rel, got, tc.deny)
		}
	}
}

func TestCredentialFilePathsEnumeratesExistingOnly(t *testing.T) {
	home := credHome(t, ".ssh/id_ed25519", ".ssh/id_ed25519.pub", ".ssh/known_hosts", ".netrc")
	got := CredentialFilePaths()

	for _, want := range []string{".ssh/id_ed25519", ".netrc"} {
		if !slices.Contains(got, filepath.Join(home, want)) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, unwanted := range []string{".ssh/id_ed25519.pub", ".ssh/known_hosts", ".aws/credentials"} {
		if slices.Contains(got, filepath.Join(home, unwanted)) {
			t.Errorf("unexpected %s in %v", unwanted, got)
		}
	}
}

func TestCredentialProtectionOptOutOpensEverything(t *testing.T) {
	home := credHome(t, ".ssh/id_ed25519")
	SetProtectCredentialFiles(false)
	t.Cleanup(func() { SetProtectCredentialFiles(true) })

	if CredentialReadPath(filepath.Join(home, ".ssh/id_ed25519")) {
		t.Error("opt-out still denied the read")
	}
	if got := CredentialFilePaths(); len(got) != 0 {
		t.Errorf("opt-out still enumerated %v", got)
	}
}

// A caller that never touches the toggle must be protected: the zero value of
// the backing flag has to mean "on", or a surface that forgets to wire config
// silently runs unprotected.
func TestCredentialProtectionDefaultsOn(t *testing.T) {
	if !ProtectCredentialFiles() {
		t.Fatal("credential protection is off before any setter ran")
	}
}

func TestSensitiveFileNameIsAboutTheNameNotTheDirectory(t *testing.T) {
	for name, want := range map[string]bool{
		".env": true, ".ENV": true, ".netrc": true, ".git-credentials": true,
		"server.pem": true, "id.KEY": true, "bundle.p12": true, "cert.pfx": true,
		"id_ed25519": true, "id_rsa": true, "ID_ECDSA": true, "id_ed25519.pub": false,
		".env.production": true, ".env.local": true, "env.go": false, ".env.example": false, ".env.sample": false, "keyboard.go": false, "monkey.txt": false,
	} {
		if got := SensitiveFileName(name); got != want {
			t.Errorf("SensitiveFileName(%q) = %v, want %v", name, got, want)
		}
	}
}
