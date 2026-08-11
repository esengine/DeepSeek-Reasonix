package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestNormalizeLegacyQQConnectionIsIdempotentAndSecretFree(t *testing.T) {
	cfg := Default()
	cfg.Bot.QQ = QQBotConfig{Enabled: true, AppID: "app-id", AppSecretEnv: "QQ_SECRET"}
	if !NormalizeLegacyQQConnection(cfg) || NormalizeLegacyQQConnection(cfg) {
		t.Fatal("legacy QQ normalization was not exactly-once")
	}
	if len(cfg.Bot.Connections) != 1 || cfg.Bot.Connections[0].Protocol != "official" || cfg.Bot.Connections[0].Credential.AppSecretEnv != "QQ_SECRET" {
		t.Fatalf("connection = %+v", cfg.Bot.Connections)
	}
	body := RenderTOML(cfg)
	if strings.Contains(body, "secret-value") || !strings.Contains(body, "protocol = \"official\"") {
		t.Fatalf("rendered QQ v2 config = %s", body)
	}
	var roundTrip Config
	if _, err := toml.Decode(body, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Bot.Connections) != 1 || roundTrip.Bot.Connections[0].Credential.AppID != "app-id" {
		t.Fatalf("round trip connections = %+v", roundTrip.Bot.Connections)
	}
}

func TestRenderLegacyQQEmitsConnectionMirror(t *testing.T) {
	cfg := Default()
	cfg.Bot.QQ = QQBotConfig{Enabled: true, AppID: "legacy-app", AppSecretEnv: "QQ_SECRET"}
	body := RenderTOML(cfg)
	if !strings.Contains(body, "config_version = 6") || !strings.Contains(body, "[[bot.connections]]") || !strings.Contains(body, "protocol = \"official\"") {
		t.Fatalf("legacy QQ render did not include v2 mirror: %s", body)
	}
	if len(cfg.Bot.Connections) != 0 {
		t.Fatal("render mutated the caller config")
	}
}

func TestNormalizeLegacyQQConnectionCoexistsWithOneBot(t *testing.T) {
	cfg := Default()
	cfg.Bot.QQ = QQBotConfig{Enabled: true, AppID: "official-app", AppSecretEnv: "QQ_SECRET"}
	cfg.Bot.Connections = []BotConnectionConfig{{
		ID: "qq-personal", Provider: "onebot", Protocol: "onebot-v11", Enabled: true,
		OneBot: OneBotConnectionOptions{WebSocketURL: "ws://127.0.0.1:3001"},
	}}
	if !NormalizeLegacyQQConnection(cfg) {
		t.Fatal("legacy official QQ connection was suppressed by OneBot")
	}
	if len(cfg.Bot.Connections) != 2 || cfg.Bot.Connections[1].Protocol != "official" {
		t.Fatalf("connections = %+v, want OneBot plus official QQ", cfg.Bot.Connections)
	}
}
