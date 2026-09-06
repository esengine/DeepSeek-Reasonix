package boot

import (
	"context"
	"os"
	"path/filepath"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"strings"
	"testing"
)

func TestBuildRestoresDeepSeekChatDefaultWithOneNotice(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "reasonix-home"))
	userPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`config_version = 7
default_model = "deepseek-flash/deepseek-v4-flash"

[[providers]]
name = "deepseek-flash"
kind = "anthropic"
base_url = "https://api.deepseek.com/anthropic"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var notices []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	})
	build := func() {
		t.Helper()
		ctrl, err := Build(context.Background(), Options{Sink: sink, WorkspaceRoot: t.TempDir()})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		ctrl.Close()
	}

	build()
	migrationNotices := 0
	for _, notice := range notices {
		if notice.Text != "User configuration was upgraded." {
			continue
		}
		migrationNotices++
		if notice.Level != event.LevelInfo || !strings.Contains(notice.Detail, "prefix-cache") {
			t.Fatalf("migration notice = %+v", notice)
		}
	}
	if migrationNotices != 1 {
		t.Fatalf("migration notices = %d, want 1; got %+v", migrationNotices, notices)
	}
	raw, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `kind = "openai"`) ||
		!strings.Contains(string(raw), `base_url = "https://api.deepseek.com"`) {
		t.Fatalf("legacy DeepSeek protocol remained on disk:\n%s", raw)
	}

	notices = nil
	build()
	for _, notice := range notices {
		if strings.Contains(notice.Text, "User configuration was upgraded") {
			t.Fatalf("second boot repeated migration notice: %+v", notice)
		}
	}
}
