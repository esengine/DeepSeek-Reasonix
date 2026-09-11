package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

func TestBuildScopesMalformedConfigDeepSeekMigrationWarningToNonDesktopFrontends(t *testing.T) {
	tests := []struct {
		name           string
		statsSource    string
		handleWarnings bool
		wantNotice     bool
	}{
		{name: "desktop accepts persistent config warning", statsSource: "desktop", handleWarnings: true},
		{name: "desktop without warning handler keeps boot warning", statsSource: "desktop", wantNotice: true},
		{name: "CLI keeps boot warning", statsSource: "cli", wantNotice: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := isolateConfigHome(t)
			t.Setenv("REASONIX_HOME", filepath.Join(home, "reasonix-home"))
			userPath := config.UserConfigPath()
			if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
				t.Fatal(err)
			}
			raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[[plugins]]
name = "windows-mcp"
command = "C:\Users\reasonix\mcp.exe"
`
			if err := os.WriteFile(userPath, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}

			var notices []event.Event
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.Notice {
					notices = append(notices, e)
				}
			})
			var handledWarnings []string
			opts := Options{Sink: sink, WorkspaceRoot: t.TempDir(), StatsSource: tt.statsSource}
			if tt.handleWarnings {
				opts.OnConfigLoadWarnings = func(warnings []string) bool {
					handledWarnings = append([]string(nil), warnings...)
					return true
				}
			}
			ctrl, err := Build(context.Background(), opts)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			ctrl.Close()

			var migrationNotice *event.Event
			for i := range notices {
				if notices[i].Text == "DeepSeek protocol migration did not complete." {
					migrationNotice = &notices[i]
					break
				}
			}
			if got := migrationNotice != nil; got != tt.wantNotice {
				t.Fatalf("migration notice present = %v, want %v; notices=%+v", got, tt.wantNotice, notices)
			}
			if got := len(handledWarnings) > 0; got != tt.handleWarnings {
				t.Fatalf("config warnings handled = %v, want %v; warnings=%v", got, tt.handleWarnings, handledWarnings)
			}
			if migrationNotice != nil &&
				(migrationNotice.Level != event.LevelWarn || !strings.Contains(migrationNotice.Detail, "toml:")) {
				t.Fatalf("migration notice lost parse diagnostics: %+v", *migrationNotice)
			}
			next, err := os.ReadFile(userPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(next) != raw {
				t.Fatalf("build rewrote malformed config:\n%s", next)
			}
		})
	}
}

func TestBuildDeliversProjectConfigWarningsWithoutDeepSeekMigrationError(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "reasonix-home"))
	workspace := t.TempDir()
	projectPath := filepath.Join(workspace, "reasonix.toml")
	raw := `[[plugins]]
name = "windows-mcp"
command = "C:\Users\reasonix\mcp.exe"
`
	if err := os.WriteFile(projectPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	var handledWarnings []string
	var notices []event.Event
	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot: workspace,
		StatsSource:   "desktop",
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
		OnConfigLoadWarnings: func(warnings []string) bool {
			handledWarnings = append([]string(nil), warnings...)
			return true
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctrl.Close()

	if len(handledWarnings) == 0 || !strings.Contains(strings.Join(handledWarnings, "\n"), "project config") {
		t.Fatalf("project config warnings were not delivered: %v", handledWarnings)
	}
	for _, notice := range notices {
		if notice.Text == "DeepSeek protocol migration did not complete." {
			t.Fatalf("project config damage produced an unrelated migration notice: %+v", notice)
		}
	}
	next, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(next) != raw {
		t.Fatalf("build rewrote malformed project config:\n%s", next)
	}
}
