package botruntime

import (
	"log/slog"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

func TestNextcloudTalkConnectionIsEnabledAndBound(t *testing.T) {
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID:       "nextcloud-talk",
		Provider: string(bot.PlatformNextcloudTalk),
		Domain:   "nextcloud-talk",
		Enabled:  true,
		Credential: config.BotConnectionCredential{
			ServerURL:   "https://cloud.example.com",
			ListenAddr:  "127.0.0.1:38017",
			WebhookPath: "/reasonix/nextcloud-talk",
			SecretEnv:   "NEXTCLOUD_TALK_BOT_SECRET",
		},
	}}

	enabled, warnings := EnabledPlatforms(cfg, []string{"nextcloud-talk"})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if !enabled[bot.PlatformNextcloudTalk] {
		t.Fatal("nextcloud-talk was not enabled")
	}

	bindings := AdapterBindings(cfg, enabled, nil, slog.Default())
	if len(bindings) != 1 {
		t.Fatalf("bindings = %d, want 1", len(bindings))
	}
	if bindings[0].Platform != bot.PlatformNextcloudTalk {
		t.Fatalf("platform = %q", bindings[0].Platform)
	}
	if bindings[0].ID != "nextcloud-talk" {
		t.Fatalf("id = %q", bindings[0].ID)
	}
	if got := bindings[0].Adapter.Name(); got != "nextcloud-talk" {
		t.Fatalf("adapter name = %q", got)
	}
}
