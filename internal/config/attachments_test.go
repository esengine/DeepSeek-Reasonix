package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAttachmentStreamLimitDefaultsAndUserValues(t *testing.T) {
	if DefaultAttachmentStreamBytes != 1<<30 {
		t.Fatal("attachment disk safety default must be 1 GiB")
	}
	for _, tc := range []struct {
		name   string
		value  *int64
		bytes  int64
		source string
	}{
		{"unset", nil, 1 << 30, "default"},
		{"negative", attachmentBytes(-1), 1 << 30, "default"},
		{"unlimited", attachmentBytes(0), 0, "user"},
		{"custom", attachmentBytes(4096), 4096, "user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Attachments.MaxStreamBytes = tc.value
			if got := cfg.AttachmentStreamLimit(); got != (AttachmentLimit{Bytes: tc.bytes, Source: tc.source}) {
				t.Fatalf("limit=%+v", got)
			}
		})
	}
	var cfg *Config
	if got := cfg.AttachmentStreamLimit(); got != (AttachmentLimit{Bytes: 1 << 30, Source: "default"}) {
		t.Fatalf("nil config limit=%+v", got)
	}
}

func TestAttachmentStreamLimitCannotBeSetByProject(t *testing.T) {
	for _, user := range []string{"", "0", "8"} {
		for _, project := range []string{"0", "1", "100"} {
			t.Run(user+"/"+project, func(t *testing.T) {
				body := ""
				if user != "" {
					body = "[attachments]\nmax_stream_bytes = " + user + "\n"
				}
				cfg, _ := loadScoped(t, body, "[attachments]\nmax_stream_bytes = "+project+"\n")
				want := AttachmentLimit{Bytes: 1 << 30, Source: "default"}
				if user != "" {
					want = AttachmentLimit{Bytes: 8, Source: "user"}
					if user == "0" {
						want.Bytes = 0
					}
				}
				if cfg.AttachmentStreamLimit() != want {
					t.Fatalf("project changed user limit: %+v", cfg.AttachmentStreamLimit())
				}
				found := false
				for _, ignored := range cfg.IgnoredProjectSettings() {
					found = found || ignored.Key == "attachments.max_stream_bytes" && ignored.Reason == ProjectUserOnly
				}
				if user != project && !found {
					t.Fatalf("missing user-only diagnostic: %+v", cfg.IgnoredProjectSettings())
				}
			})
		}
	}
}

func TestAttachmentStreamLimitRoundTripAndHeldPointerIsolation(t *testing.T) {
	for _, value := range []int64{0, 98765} {
		cfg := Default()
		cfg.Attachments.MaxStreamBytes = attachmentBytes(value)
		body := RenderTOMLForScope(cfg, RenderScopeUser)
		var decoded Config
		if _, err := toml.Decode(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if got := decoded.AttachmentStreamLimit(); got != (AttachmentLimit{Bytes: value, Source: "user"}) {
			t.Fatalf("roundtrip=%+v", got)
		}
		if project := RenderTOMLForScope(cfg, RenderScopeProject); strings.Contains(project, "[attachments]") {
			t.Fatal("project rendering persisted a user-only limit")
		}
		held := holdUserScope(cfg)
		*cfg.Attachments.MaxStreamBytes = value + 1
		held.narrow(cfg, t.TempDir())
		if *cfg.Attachments.MaxStreamBytes != value {
			t.Fatal("project merge mutated held user limit through shared pointer")
		}
	}
	if body := RenderTOMLForScope(Default(), RenderScopeUser); strings.Contains(body, "[attachments]") {
		t.Fatal("default limit was materialized as a user choice")
	}
}

func attachmentBytes(value int64) *int64 { return &value }

func TestLoadAttachmentStreamLimitStrictUserRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	for _, value := range []string{"0", "2048"} {
		body := []byte("[attachments]\nmax_stream_bytes = " + value + "\n")
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		limit, err := LoadAttachmentStreamLimit()
		if err != nil || limit.Source != "user" || fmt.Sprint(limit.Bytes) != value {
			t.Fatalf("limit=%+v err=%v", limit, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(body) {
			t.Fatal("strict upload read rewrote user configuration", err)
		}
	}
	if err := os.WriteFile(path, []byte("[attachments]\nmax_stream_bytes = [broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAttachmentStreamLimit(); err == nil {
		t.Fatal("strict upload read recovered a malformed config to wider defaults")
	}
}

func ExampleConfig_AttachmentStreamLimit() {
	cfg := Default()
	fmt.Println(cfg.AttachmentStreamLimit())
	// Output: {1073741824 default}
}

func TestAttachmentLimitSavePreservesUserChoice(t *testing.T) {
	for _, value := range []int64{0, 19} {
		home := t.TempDir()
		t.Setenv("REASONIX_HOME", home)
		path := filepath.Join(home, "config.toml")
		cfg := Default()
		cfg.Attachments.MaxStreamBytes = &value
		cfg.UI.Theme = "dark"
		if err := cfg.SaveTo(path); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadForEditReadOnlyStrict(path)
		if err != nil || loaded.AttachmentStreamLimit() != cfg.AttachmentStreamLimit() {
			t.Fatalf("saved limit=%+v err=%v", loaded.AttachmentStreamLimit(), err)
		}
		if loaded.UI.Theme != "dark" {
			t.Fatal("existing user theme lost")
		}
	}
}
