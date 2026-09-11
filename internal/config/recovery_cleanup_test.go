package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryCopyAutoCleanupEnabled(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "unset defaults on", content: "", want: true},
		{name: "explicit on", content: "[recovery_cleanup]\nauto_enabled = true\n", want: true},
		{name: "explicit off", content: "[recovery_cleanup]\nauto_enabled = false\n", want: false},
		{name: "unrelated table keeps default", content: "[history_search]\nmax_mb = 64\n", want: true},
		{name: "malformed keeps default", content: "[recovery_cleanup\n", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("REASONIX_HOME", home)
			if tc.content != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := RecoveryCopyAutoCleanupEnabled(); got != tc.want {
				t.Fatalf("RecoveryCopyAutoCleanupEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
