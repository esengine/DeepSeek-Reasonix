package config

import (
	"strings"
	"testing"
)

func TestRenderBotCredentialNextcloudTalk(t *testing.T) {
	got := renderBotCredential(BotConnectionCredential{
		ServerURL:   "https://cloud.example.com",
		ListenAddr:  "127.0.0.1:38017",
		WebhookPath: "/reasonix/nextcloud-talk",
		SecretEnv:   "NEXTCLOUD_TALK_BOT_SECRET",
	})
	for _, want := range []string{
		`server_url = "https://cloud.example.com"`,
		`listen_addr = "127.0.0.1:38017"`,
		`webhook_path = "/reasonix/nextcloud-talk"`,
		`secret_env = "NEXTCLOUD_TALK_BOT_SECRET"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderBotCredential() = %q, missing %q", got, want)
		}
	}
}
