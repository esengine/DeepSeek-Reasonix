package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSharePortRange(t *testing.T) {
	for _, bad := range []int{-1, 1, 1023, 65536, 70000} {
		if err := ValidateSharePort(bad); !errors.Is(err, ErrSharePortOutOfRange) {
			t.Fatalf("ValidateSharePort(%d) = %v, want ErrSharePortOutOfRange", bad, err)
		}
	}
	for _, ok := range []int{0, 1024, 41234, 65535} {
		if err := ValidateSharePort(ok); err != nil {
			t.Fatalf("ValidateSharePort(%d) = %v", ok, err)
		}
	}
}

func TestSharePortAccessorIgnoresAnOutOfRangeValue(t *testing.T) {
	c := &Config{Serve: ServeConfig{SharePort: 80}}
	if got := c.SharePort(); got != 0 {
		t.Fatalf("SharePort with a hand-written 80 = %d, want 0 (random)", got)
	}
	c.Serve.SharePort = 41234
	if got := c.SharePort(); got != 41234 {
		t.Fatalf("SharePort = %d, want 41234", got)
	}
}

func TestSharePortSurvivesASaveAndKeepsTheRestOfServe(t *testing.T) {
	isolateUserConfigHome(t)
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[serve]\nauth_mode = \"token\"\ntoken = \"keep-me\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadForEdit(path)
	if err := cfg.SetSharePort(41234); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	back := LoadForEdit(path)
	if back.Serve.SharePort != 41234 || back.Serve.Token != "keep-me" || back.Serve.AuthMode != "token" {
		t.Fatalf("after save [serve] = %+v, want share_port 41234 with the token kept", back.Serve)
	}
	if err := back.SetSharePort(0); err != nil {
		t.Fatal(err)
	}
	if err := back.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if again := LoadForEdit(path); again.Serve.SharePort != 0 || again.Serve.Token != "keep-me" {
		t.Fatalf("after clearing [serve] = %+v, want random port and the token kept", again.Serve)
	}
}

func TestProjectConfigCannotPinTheSharePort(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[serve]\nshare_port = 41234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SharePort() != 0 {
		t.Fatalf("project reasonix.toml pinned the share port to %d", cfg.SharePort())
	}
}
