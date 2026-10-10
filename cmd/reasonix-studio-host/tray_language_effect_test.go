package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/serve"
)

func TestAssembledTrayLanguage(t *testing.T) {
	before := i18n.CurrentLanguage()
	t.Cleanup(func() { i18n.DetectLanguage(before) })
	for _, tt := range []struct{ name, system, desktop, env, locale, want string }{
		{"system", "zh-Hans-CN", "auto", "", "", "zh"},
		{"traditional", "zh-Hant-TW", "auto", "", "", "zh-TW"},
		{"desktop", "zh-Hans-CN", "en", "", "", "en"},
		{"reasonix", "zh-Hans-CN", "auto", "en-US", "", "en"},
		{"desktop-over-env", "en-US", "zh", "en", "", "zh"},
		{"c-locale", "zh-Hans-CN", "auto", "", "C", "en"},
		{"posix-locale", "zh-Hans-CN", "auto", "", "POSIX.UTF-8", "en"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", testenv.TempDir(t))
			t.Chdir(testenv.TempDir(t))
			for _, key := range []string{"REASONIX_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
				t.Setenv(key, "")
			}
			t.Setenv("REASONIX_SYSTEM_LANG", tt.system)
			t.Setenv("REASONIX_LANG", tt.env)
			t.Setenv("LANG", tt.locale)
			cfg := config.Default()
			disabled := false
			cfg.Desktop.Telemetry, cfg.Desktop.Metrics = &disabled, &disabled
			if err := cfg.SetDesktopLanguage(tt.desktop); err != nil {
				t.Fatal(err)
			}
			if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			hub, err := assemble(ctx, io.Discard, io.Discard, shellIdentity{}, nil, newStartupPhases(io.Discard, time.Now))
			if err != nil {
				t.Fatalf("assemble: %v", err)
			}
			defer hub.Shutdown()
			b, err := bind(hub.Handler())
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- b.serve(ctx) }()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(30 * time.Second):
					t.Error("host did not drain")
				}
			}()
			check := func(want string) {
				t.Helper()
				status, body := ask(t, b, http.MethodGet, "/tray/state", nil)
				if status != http.StatusOK {
					t.Fatalf("tray status = %d", status)
				}
				var state serve.TrayState
				if err := json.Unmarshal([]byte(body), &state); err != nil {
					t.Fatal(err)
				}
				say := i18n.Catalog(want)
				labels := serve.TrayLabels{Open: say.TrayOpen, CloseToTray: say.TrayCloseToTray, Quit: say.TrayQuit}
				if state.Labels != labels || state.Line != say.TrayIdle {
					t.Fatalf("tray = %+v, want labels %+v and line %q", state, labels, say.TrayIdle)
				}
			}
			check(tt.want)
			for _, lang := range []string{"en", "zh"} {
				status, _ := ask(t, b, http.MethodPost, "/appearance", func(r *http.Request) {
					r.Body = io.NopCloser(strings.NewReader(`{"language":"` + lang + `"}`))
				})
				if status != http.StatusOK {
					t.Fatalf("appearance status = %d", status)
				}
				check(lang)
			}
		})
	}
}
