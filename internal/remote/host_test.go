package remote

import (
	"testing"

	"reasonix/internal/config"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in         string
		user, host string
		port       int
		wantErr    bool
	}{
		{"host", "", "host", 0, false},
		{"user@host", "user", "host", 0, false},
		{"user@host:2222", "user", "host", 2222, false},
		{"host:22", "", "host", 22, false},
		{"[::1]:22", "", "::1", 22, false},
		{"dev@[2001:db8::1]:2200", "dev", "2001:db8::1", 2200, false},
		{"2001:db8::1", "", "2001:db8::1", 0, false},
		{"", "", "", 0, true},
		{"user@", "", "", 0, true},
		{"host:99999", "", "", 0, true},
		{"host:abc", "", "", 0, true},
	}
	for _, c := range cases {
		u, h, p, err := ParseTarget(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseTarget(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTarget(%q): %v", c.in, err)
			continue
		}
		if u != c.user || h != c.host || p != c.port {
			t.Errorf("ParseTarget(%q) = (%q,%q,%d), want (%q,%q,%d)", c.in, u, h, p, c.user, c.host, c.port)
		}
	}
}

func TestResolveHostFromConfig(t *testing.T) {
	cfg := config.Default()
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{
		Name:      "box",
		Host:      "10.0.0.5",
		Port:      2200,
		User:      "dev",
		ProxyJump: "bastion, second",
		Workspace: "~/app",
	}); err != nil {
		t.Fatal(err)
	}
	h, err := ResolveHost(cfg, "box", nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.HostName != "10.0.0.5" || h.Port != 2200 || h.User != "dev" {
		t.Fatalf("resolved wrong: %+v", h)
	}
	if len(h.ProxyJump) != 2 || h.ProxyJump[0] != "bastion" || h.ProxyJump[1] != "second" {
		t.Fatalf("proxy jump chain wrong: %v", h.ProxyJump)
	}
	if h.Addr() != "10.0.0.5:2200" {
		t.Fatalf("Addr = %q", h.Addr())
	}
}

func TestResolveHostAdHocDefaultsPort(t *testing.T) {
	h, err := ResolveHost(config.Default(), "user@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.Port != 22 {
		t.Fatalf("default port = %d, want 22", h.Port)
	}
	if h.User != "user" {
		t.Fatalf("user = %q", h.User)
	}
}
