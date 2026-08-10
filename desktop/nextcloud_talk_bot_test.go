package main

import (
	"testing"

	"reasonix/internal/config"
)

func TestNextcloudTalkConnectionCredentialRoundTrip(t *testing.T) {
	t.Setenv("NEXTCLOUD_TALK_BOT_SECRET", "secret")
	view := BotConnectionView{
		ID:       "nextcloud-talk",
		Provider: "nextcloud-talk",
		Domain:   "nextcloud-talk",
		Enabled:  true,
		Credential: BotConnectionCredentialView{
			ServerURL:   "https://cloud.example.com",
			ListenAddr:  "127.0.0.1:38017",
			WebhookPath: "/reasonix/nextcloud-talk",
			SecretEnv:   "NEXTCLOUD_TALK_BOT_SECRET",
		},
	}
	configs := botConnectionConfigs([]BotConnectionView{view})
	if len(configs) != 1 {
		t.Fatalf("configs = %d, want 1", len(configs))
	}
	cred := configs[0].Credential
	if cred.ServerURL != "https://cloud.example.com" || cred.ListenAddr != "127.0.0.1:38017" ||
		cred.WebhookPath != "/reasonix/nextcloud-talk" || cred.SecretEnv != "NEXTCLOUD_TALK_BOT_SECRET" {
		t.Fatalf("credential = %+v", cred)
	}
	if !botCredentialSecretSet(config.BotConnectionConfig{
		Provider:   "nextcloud-talk",
		Credential: cred,
	}) {
		t.Fatal("expected Nextcloud Talk secret to be detected")
	}
}
