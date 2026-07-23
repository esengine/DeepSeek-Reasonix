package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveYOLOPolicyConfigPrecedence(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, user, project          string
		wantEffective, wantExpansion bool
	}{
		{name: "default"},
		{name: "user true", user: "[sandbox]\nyolo_auto_approve_capabilities = true\n", wantEffective: true},
		{name: "project expansion", user: "[sandbox]\nyolo_auto_approve_capabilities = false\n", project: "[sandbox]\nyolo_auto_approve_capabilities = true\n", wantEffective: true, wantExpansion: true},
		{name: "already user true", user: "[sandbox]\nyolo_auto_approve_capabilities = true\n", project: "[sandbox]\nyolo_auto_approve_capabilities = true\n", wantEffective: true},
		{name: "project disables", user: "[sandbox]\nyolo_auto_approve_capabilities = true\n", project: "[sandbox]\nyolo_auto_approve_capabilities = false\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(UserConfigPath())
			_ = os.Remove(filepath.Join(root, "reasonix.toml"))
			if tc.user != "" {
				if err := os.WriteFile(UserConfigPath(), []byte(tc.user), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.project != "" {
				if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(tc.project), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := ResolveYOLOPolicyConfig(root)
			if got.Effective != tc.wantEffective || got.ProjectExpansion != tc.wantExpansion {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}
