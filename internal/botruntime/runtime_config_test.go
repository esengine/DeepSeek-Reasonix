package botruntime

import (
	"reflect"
	"testing"

	"reasonix/internal/bot"
	"reasonix/internal/config"
)

func TestEnabledPlatforms(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		channels []string
		want     map[bot.Platform]bool
		wantWarn []string
	}{
		{
			name:     "no channels with nil config disables every platform",
			cfg:      nil,
			channels: nil,
			want:     map[bot.Platform]bool{bot.PlatformQQ: false, bot.PlatformFeishu: false, bot.PlatformWeixin: false},
		},
		{
			name: "no channels resolves configured platforms",
			cfg:  &config.Config{Bot: config.BotConfig{QQ: config.QQBotConfig{Enabled: true}}},
			want: map[bot.Platform]bool{bot.PlatformQQ: true, bot.PlatformFeishu: false, bot.PlatformWeixin: false},
		},
		{
			name:     "explicit channel keeps only that platform key",
			cfg:      &config.Config{Bot: config.BotConfig{QQ: config.QQBotConfig{Enabled: true}}},
			channels: []string{"qq"},
			want:     map[bot.Platform]bool{bot.PlatformQQ: true},
		},
		{
			name:     "unconfigured requested channel still keys the platform as disabled",
			cfg:      &config.Config{},
			channels: []string{"qq"},
			want:     map[bot.Platform]bool{bot.PlatformQQ: false},
		},
		{
			name:     "feishu channel",
			cfg:      &config.Config{Bot: config.BotConfig{Feishu: config.FeishuBotConfig{Enabled: true}}},
			channels: []string{"feishu"},
			want:     map[bot.Platform]bool{bot.PlatformFeishu: true},
		},
		{
			name:     "lark alias enables the feishu platform",
			cfg:      &config.Config{Bot: config.BotConfig{Feishu: config.FeishuBotConfig{Enabled: true}}},
			channels: []string{"lark"},
			want:     map[bot.Platform]bool{bot.PlatformFeishu: true},
		},
		{
			name:     "lark alias is case and space insensitive",
			cfg:      &config.Config{Bot: config.BotConfig{Feishu: config.FeishuBotConfig{Enabled: true}}},
			channels: []string{" LARK ", " lark "},
			want:     map[bot.Platform]bool{bot.PlatformFeishu: true},
		},
		{
			name:     "unknown channel is reported as warning",
			cfg:      &config.Config{},
			channels: []string{"slack"},
			want:     map[bot.Platform]bool{},
			wantWarn: []string{"slack"},
		},
		{
			name:     "blank channels are ignored silently",
			cfg:      &config.Config{},
			channels: []string{"", "  "},
			want:     map[bot.Platform]bool{},
		},
		{
			name:     "mixed known and unknown channels",
			cfg:      &config.Config{},
			channels: []string{"qq", "slack", ""},
			want:     map[bot.Platform]bool{bot.PlatformQQ: false},
			wantWarn: []string{"slack"},
		},
		{
			name:     "enabled connection counts as configured",
			cfg:      &config.Config{Bot: config.BotConfig{Connections: []config.BotConnectionConfig{{Provider: "feishu", Enabled: true}}}},
			channels: []string{"feishu"},
			want:     map[bot.Platform]bool{bot.PlatformFeishu: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, warnings := EnabledPlatforms(tt.cfg, tt.channels)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EnabledPlatforms(%v, %v) enabled = %v, want %v", tt.cfg, tt.channels, got, tt.want)
			}
			if !reflect.DeepEqual(warnings, tt.wantWarn) {
				t.Fatalf("EnabledPlatforms(%v, %v) warnings = %v, want %v", tt.cfg, tt.channels, warnings, tt.wantWarn)
			}
		})
	}
}

func TestRequestedFeishuDomains(t *testing.T) {
	tests := []struct {
		name     string
		channels []string
		want     map[string]bool
	}{
		{name: "nil channels", channels: nil, want: nil},
		{name: "empty channels", channels: []string{}, want: nil},
		{name: "feishu requested", channels: []string{"feishu"}, want: map[string]bool{"feishu": true}},
		{name: "lark requested", channels: []string{"lark"}, want: map[string]bool{"lark": true}},
		{name: "case and space insensitive", channels: []string{" FEISHU ", " Lark "}, want: map[string]bool{"feishu": true, "lark": true}},
		{name: "no feishu family channel returns nil", channels: []string{"qq", "slack", ""}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequestedFeishuDomains(tt.channels); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("RequestedFeishuDomains(%v) = %v, want %v", tt.channels, got, tt.want)
			}
		})
	}
}

func TestFeishuDomainKey(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{name: "lark", domain: "lark", want: "lark"},
		{name: "lark uppercase", domain: "LARK", want: "lark"},
		{name: "lark padded", domain: "  lark  ", want: "lark"},
		{name: "feishu", domain: "feishu", want: "feishu"},
		{name: "empty defaults to feishu", domain: "", want: "feishu"},
		{name: "unknown domain defaults to feishu", domain: "weixin", want: "feishu"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := feishuDomainKey(tt.domain); got != tt.want {
				t.Fatalf("feishuDomainKey(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestHasEnabledPlatform(t *testing.T) {
	tests := []struct {
		name    string
		enabled map[bot.Platform]bool
		want    bool
	}{
		{name: "nil map", enabled: nil, want: false},
		{name: "empty map", enabled: map[bot.Platform]bool{}, want: false},
		{name: "all disabled", enabled: map[bot.Platform]bool{bot.PlatformQQ: false, bot.PlatformFeishu: false}, want: false},
		{name: "one enabled among disabled", enabled: map[bot.Platform]bool{bot.PlatformQQ: false, bot.PlatformFeishu: true}, want: true},
		{name: "single enabled platform", enabled: map[bot.Platform]bool{bot.PlatformWeixin: true}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasEnabledPlatform(tt.enabled); got != tt.want {
				t.Fatalf("HasEnabledPlatform(%v) = %v, want %v", tt.enabled, got, tt.want)
			}
		})
	}
}

func TestPlatformConfigured(t *testing.T) {
	withConnection := func(provider string, enabled bool) *config.Config {
		return &config.Config{Bot: config.BotConfig{Connections: []config.BotConnectionConfig{{Provider: provider, Enabled: enabled}}}}
	}
	tests := []struct {
		name     string
		cfg      *config.Config
		platform bot.Platform
		want     bool
	}{
		{name: "nil config qq", cfg: nil, platform: bot.PlatformQQ, want: false},
		{name: "nil config feishu", cfg: nil, platform: bot.PlatformFeishu, want: false},
		{name: "qq enabled in config", cfg: &config.Config{Bot: config.BotConfig{QQ: config.QQBotConfig{Enabled: true}}}, platform: bot.PlatformQQ, want: true},
		{name: "feishu enabled in config", cfg: &config.Config{Bot: config.BotConfig{Feishu: config.FeishuBotConfig{Enabled: true}}}, platform: bot.PlatformFeishu, want: true},
		{name: "weixin enabled in config", cfg: &config.Config{Bot: config.BotConfig{Weixin: config.WeixinBotConfig{Enabled: true}}}, platform: bot.PlatformWeixin, want: true},
		{name: "nothing enabled", cfg: &config.Config{}, platform: bot.PlatformWeixin, want: false},
		{name: "enabled connection counts", cfg: withConnection("feishu", true), platform: bot.PlatformFeishu, want: true},
		{name: "padded connection provider is trimmed", cfg: withConnection("  feishu ", true), platform: bot.PlatformFeishu, want: true},
		{name: "disabled connection does not count", cfg: withConnection("qq", false), platform: bot.PlatformQQ, want: false},
		{name: "unknown provider never counts", cfg: withConnection("slack", true), platform: bot.PlatformFeishu, want: false},
		{name: "connection does not count for other platform", cfg: withConnection("qq", true), platform: bot.PlatformFeishu, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlatformConfigured(tt.cfg, tt.platform); got != tt.want {
				t.Fatalf("PlatformConfigured(%v, %q) = %v, want %v", tt.cfg, tt.platform, got, tt.want)
			}
		})
	}
}

func TestBotAccessActive(t *testing.T) {
	tests := []struct {
		name   string
		access config.BotAccessConfig
		want   bool
	}{
		{name: "zero value is inactive", access: config.BotAccessConfig{}, want: false},
		{name: "enabled flag", access: config.BotAccessConfig{Enabled: true}, want: true},
		{name: "allow all", access: config.BotAccessConfig{AllowAll: true}, want: true},
		{name: "pairing enabled", access: config.BotAccessConfig{PairingEnabled: true}, want: true},
		{name: "users", access: config.BotAccessConfig{Users: []string{"u1"}}, want: true},
		{name: "groups", access: config.BotAccessConfig{Groups: []string{"g1"}}, want: true},
		{name: "approvers", access: config.BotAccessConfig{Approvers: []string{"a1"}}, want: true},
		{name: "admins", access: config.BotAccessConfig{Admins: []string{"ad1"}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BotAccessActive(tt.access); got != tt.want {
				t.Fatalf("BotAccessActive(%+v) = %v, want %v", tt.access, got, tt.want)
			}
		})
	}
}

func TestConnectionRuntimeID(t *testing.T) {
	tests := []struct {
		name string
		conn config.BotConnectionConfig
		want string
	}{
		{name: "explicit id wins", conn: config.BotConnectionConfig{ID: "feishu-lark", Provider: "feishu", Domain: "lark"}, want: "feishu-lark"},
		{name: "explicit id is trimmed", conn: config.BotConnectionConfig{ID: "  feishu-lark  "}, want: "feishu-lark"},
		{name: "provider and domain join", conn: config.BotConnectionConfig{Provider: "feishu", Domain: "lark"}, want: "feishu-lark"},
		{name: "provider and domain are trimmed", conn: config.BotConnectionConfig{Provider: " feishu ", Domain: " lark "}, want: "feishu-lark"},
		{name: "provider only", conn: config.BotConnectionConfig{Provider: "qq"}, want: "qq"},
		{name: "blank id falls back to provider-domain", conn: config.BotConnectionConfig{ID: "  ", Provider: "qq", Domain: "qq"}, want: "qq-qq"},
		{name: "no provider yields empty", conn: config.BotConnectionConfig{Domain: "lark"}, want: ""},
		{name: "blank provider yields empty", conn: config.BotConnectionConfig{Provider: "  ", Domain: "lark"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConnectionRuntimeID(tt.conn); got != tt.want {
				t.Fatalf("ConnectionRuntimeID(%+v) = %q, want %q", tt.conn, got, tt.want)
			}
		})
	}
}

func TestModelName(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		override string
		want     string
	}{
		{name: "override wins", cfg: &config.Config{Bot: config.BotConfig{Model: "bot-model"}}, override: "override-model", want: "override-model"},
		{name: "override is trimmed", cfg: &config.Config{}, override: "  override-model  ", want: "override-model"},
		{name: "nil config with override", cfg: nil, override: "override-model", want: "override-model"},
		{name: "nil config without override", cfg: nil, override: "", want: ""},
		{name: "bot model used when no override", cfg: &config.Config{Bot: config.BotConfig{Model: "bot-model"}}, override: "", want: "bot-model"},
		{name: "bot model is trimmed", cfg: &config.Config{Bot: config.BotConfig{Model: "  bot-model  "}}, override: " ", want: "bot-model"},
		{name: "default model fallback", cfg: &config.Config{DefaultModel: "default-model"}, override: "", want: "default-model"},
		{name: "empty everywhere", cfg: &config.Config{}, override: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModelName(tt.cfg, tt.override); got != tt.want {
				t.Fatalf("ModelName(%v, %q) = %q, want %q", tt.cfg, tt.override, got, tt.want)
			}
		})
	}
}

func TestBotAccessUserCount(t *testing.T) {
	tests := []struct {
		name   string
		access config.BotAccessConfig
		want   int
	}{
		{name: "empty", access: config.BotAccessConfig{}, want: 0},
		{name: "counts every role list", access: config.BotAccessConfig{
			Users: []string{"u1", "u2"}, Groups: []string{"g1"}, Approvers: []string{"a1"}, Admins: []string{"ad1", "ad2", "ad3"},
		}, want: 7},
		{name: "only groups count too", access: config.BotAccessConfig{Groups: []string{"g1", "g2"}}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BotAccessUserCount(tt.access); got != tt.want {
				t.Fatalf("BotAccessUserCount(%+v) = %d, want %d", tt.access, got, tt.want)
			}
		})
	}
}

func TestBotConfigHasAccessControl(t *testing.T) {
	tests := []struct {
		name string
		bc   config.BotConfig
		want bool
	}{
		{name: "zero bot config", bc: config.BotConfig{}, want: false},
		{name: "allow all", bc: config.BotConfig{Allowlist: config.BotAllowlist{AllowAll: true}}, want: true},
		{name: "pairing enabled", bc: config.BotConfig{Pairing: config.BotPairingConfig{Enabled: true}}, want: true},
		{name: "allowlist enabled with users", bc: config.BotConfig{Allowlist: config.BotAllowlist{Enabled: true, QQUsers: []string{"u1"}}}, want: true},
		{name: "allowlist enabled with approvers", bc: config.BotConfig{Allowlist: config.BotAllowlist{Enabled: true, FeishuApprovers: []string{"a1"}}}, want: true},
		{name: "allowlist enabled but empty", bc: config.BotConfig{Allowlist: config.BotAllowlist{Enabled: true}}, want: false},
		{name: "allowlist groups alone do not count", bc: config.BotConfig{Allowlist: config.BotAllowlist{Enabled: true, FeishuGroups: []string{"g1"}}}, want: false},
		{name: "qq access users", bc: config.BotConfig{QQ: config.QQBotConfig{Access: config.BotAccessConfig{Users: []string{"u1"}}}}, want: true},
		{name: "enabled connection with access", bc: config.BotConfig{Connections: []config.BotConnectionConfig{{Provider: "feishu", Enabled: true, Access: config.BotAccessConfig{Admins: []string{"ad1"}}}}}, want: true},
		{name: "enabled connection without access", bc: config.BotConfig{Connections: []config.BotConnectionConfig{{Provider: "feishu", Enabled: true}}}, want: false},
		{name: "disabled connection access ignored", bc: config.BotConfig{Connections: []config.BotConnectionConfig{{Provider: "feishu", Enabled: false, Access: config.BotAccessConfig{Users: []string{"u1"}}}}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BotConfigHasAccessControl(tt.bc); got != tt.want {
				t.Fatalf("BotConfigHasAccessControl(%+v) = %v, want %v", tt.bc, got, tt.want)
			}
		})
	}
}

func TestTrimStringSlice(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []string
	}{
		{name: "nil", values: nil, want: nil},
		{name: "empty", values: []string{}, want: nil},
		{name: "single value", values: []string{"a"}, want: []string{"a"}},
		{name: "trims and drops blanks", values: []string{" a ", "b", "  ", " c "}, want: []string{"a", "b", "c"}},
		{name: "all blank yields empty non-nil", values: []string{" ", "  "}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimStringSlice(tt.values); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("trimStringSlice(%v) = %#v, want %#v", tt.values, got, tt.want)
			}
		})
	}
}

func TestChatUsesGroupAllowlist(t *testing.T) {
	tests := []struct {
		name     string
		chatType bot.ChatType
		want     bool
	}{
		{name: "group", chatType: bot.ChatGroup, want: true},
		{name: "guild", chatType: bot.ChatGuild, want: true},
		{name: "thread", chatType: bot.ChatThread, want: true},
		{name: "dm", chatType: bot.ChatDM, want: false},
		{name: "empty", chatType: "", want: false},
		{name: "direct", chatType: bot.ChatType("direct"), want: false},
		{name: "unknown", chatType: bot.ChatType("channel"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatUsesGroupAllowlist(tt.chatType); got != tt.want {
				t.Fatalf("chatUsesGroupAllowlist(%q) = %v, want %v", tt.chatType, got, tt.want)
			}
		})
	}
}

func TestAppendUniqueString(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		next    string
		want    []string
		wantNew bool
	}{
		{name: "append to nil", values: nil, next: "a", want: []string{"a"}, wantNew: true},
		{name: "append new value", values: []string{"a"}, next: "b", want: []string{"a", "b"}, wantNew: true},
		{name: "duplicate rejected", values: []string{"a"}, next: "a", want: []string{"a"}, wantNew: false},
		{name: "duplicate with padding rejected", values: []string{"a"}, next: " a ", want: []string{"a"}, wantNew: false},
		{name: "padded existing matches", values: []string{" a "}, next: "a", want: []string{" a "}, wantNew: false},
		{name: "blank next rejected", values: []string{"a"}, next: "  ", want: []string{"a"}, wantNew: false},
		{name: "empty next rejected", values: []string{"a"}, next: "", want: []string{"a"}, wantNew: false},
		{name: "case sensitive duplicates allowed", values: []string{"a"}, next: "A", want: []string{"a", "A"}, wantNew: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := appendUniqueString(tt.values, tt.next)
			if changed != tt.wantNew || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("appendUniqueString(%v, %q) = (%v, %v), want (%v, %v)", tt.values, tt.next, got, changed, tt.want, tt.wantNew)
			}
		})
	}
}

func TestFirstNonEmptyString(t *testing.T) {
	tests := []struct {
		name string
		vals []string
		want string
	}{
		{name: "no arguments", vals: nil, want: ""},
		{name: "all empty", vals: []string{"", "  "}, want: ""},
		{name: "first wins", vals: []string{"a", "b"}, want: "a"},
		{name: "skips empty prefix", vals: []string{"", "b", "c"}, want: "b"},
		{name: "skips blank prefix", vals: []string{"  ", "c"}, want: "c"},
		{name: "returns untrimmed value", vals: []string{" a "}, want: " a "},
		{name: "blank after value ignored", vals: []string{"a", "  "}, want: "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstNonEmptyString(tt.vals...); got != tt.want {
				t.Fatalf("firstNonEmptyString(%v) = %q, want %q", tt.vals, got, tt.want)
			}
		})
	}
}
