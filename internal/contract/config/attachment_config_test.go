package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAttachmentLimitIsHeldAtUserScope(t *testing.T) {
	for _, tc := range []struct {
		name, user, project, source string
		limit                       int64
	}{
		{"default", "", "[attachments]\nmax_stream_bytes=0", "default", DefaultAttachmentStreamBytes},
		{"custom", "[attachments]\nmax_stream_bytes=8", "[attachments]\nmax_stream_bytes=0", "user", 8},
		{"unlimited", "[attachments]\nmax_stream_bytes=0", "[attachments]\nmax_stream_bytes=1", "user", 0},
		{"negative", "[attachments]\nmax_stream_bytes=-1", "[attachments]\nmax_stream_bytes=0", "default", DefaultAttachmentStreamBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := loadScoped(t, tc.user, tc.project)
			if got := cfg.AttachmentStreamLimit(); got.Bytes != tc.limit || got.Source != tc.source {
				t.Fatalf("limit=%+v, want %d/%s", got, tc.limit, tc.source)
			}
			if !slices.Contains(ignoredKeys(cfg), "attachments.max_stream_bytes") {
				t.Fatalf("project setting not reported: %v", ignoredKeys(cfg))
			}
		})
	}
}

func TestAttachmentLimitUserRenderAndSaveRoundTrip(t *testing.T) {
	for _, value := range []int64{0, 19} {
		cfg := Default()
		cfg.Attachments.MaxStreamBytes = &value
		for _, scope := range []RenderScope{RenderScopeFull, RenderScopeUser} {
			var decoded Config
			if _, err := toml.Decode(RenderTOMLForScope(cfg, scope), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.AttachmentStreamLimit() != cfg.AttachmentStreamLimit() {
				t.Fatalf("%s render lost explicit value %d", scope, value)
			}
		}
		if strings.Contains(RenderTOMLForScope(cfg, RenderScopeProject), "max_stream_bytes") || strings.Contains(RenderTOMLProjectDelta(cfg), "max_stream_bytes") {
			t.Fatal("user-only limit leaked into project config")
		}
		home := t.TempDir()
		t.Setenv("REASONIX_HOME", home)
		path := filepath.Join(home, "config.toml")
		if err := os.WriteFile(path, []byte("# user's comment\n[future_feature]\nkeep=42\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cfg.SaveTo(path); err != nil {
			t.Fatal(err)
		}
		reloaded, err := LoadForEditReadOnlyStrict(path)
		if err != nil || reloaded.AttachmentStreamLimit() != cfg.AttachmentStreamLimit() {
			t.Fatalf("save/reload lost limit: %+v %v", reloaded.AttachmentStreamLimit(), err)
		}
		raw, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(raw), "[future_feature]") || !strings.Contains(string(raw), "# user's comment") {
			t.Fatalf("unrelated config lost: %v", err)
		}
	}
}

func TestAttachmentLimitStrictReadRejectsMalformedUserFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if got, err := LoadAttachmentStreamLimit(); err != nil || got.Bytes != DefaultAttachmentStreamBytes || got.Source != "default" {
		t.Fatalf("missing user config: %+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[attachments\nmax_stream_bytes=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAttachmentStreamLimit(); err == nil {
		t.Fatal("malformed safety config silently used defaults")
	}
}
