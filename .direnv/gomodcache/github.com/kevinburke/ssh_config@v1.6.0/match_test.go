package ssh_config

import (
	"strings"
	"testing"
)

func TestMatchHostBasic(t *testing.T) {
	us := &UserSettings{
		userConfigFinder: testConfigFinder("testdata/match-host"),
	}

	val := us.Get("dev.example.com", "Port")
	if val != "2222" {
		t.Errorf("expected Port=2222 for dev.example.com, got %q", val)
	}
	val = us.Get("dev.example.com", "User")
	if val != "admin" {
		t.Errorf("expected User=admin for dev.example.com, got %q", val)
	}
	val = us.Get("dev.example.com", "IdentityFile")
	if val != "~/.ssh/prod_key" {
		t.Errorf("expected IdentityFile=~/.ssh/prod_key, got %q", val)
	}
}

func TestMatchHostNoMatch(t *testing.T) {
	us := &UserSettings{
		userConfigFinder:   testConfigFinder("testdata/match-host"),
		systemConfigFinder: nullConfigFinder,
	}

	// "other.com" doesn't match *.example.com, should fall back to defaults
	val := us.Get("other.com", "Port")
	if val != "22" {
		t.Errorf("expected default Port=22 for other.com, got %q", val)
	}
	val = us.Get("other.com", "User")
	if val != "" {
		t.Errorf("expected empty User for other.com, got %q", val)
	}
}

func TestMatchHostNegation(t *testing.T) {
	us := &UserSettings{
		userConfigFinder: testConfigFinder("testdata/match-host-negation"),
	}

	// dev.example.com matches *.example.com and is not excluded by
	// !*.test.example.com, so the Match block applies.
	val := us.Get("dev.example.com", "Port")
	if val != "2222" {
		t.Errorf("expected Port=2222 for dev.example.com, got %q", val)
	}
	val = us.Get("dev.example.com", "User")
	if val != "prod" {
		t.Errorf("expected User=prod for dev.example.com, got %q", val)
	}

	// dev.test.example.com matches !*.test.example.com negation, so the
	// Match block should NOT apply. The Host block should match instead.
	val = us.Get("dev.test.example.com", "Port")
	if val != "22" {
		t.Errorf("expected Port=22 for dev.test.example.com (negated), got %q", val)
	}
	val = us.Get("dev.test.example.com", "User")
	if val != "default" {
		t.Errorf("expected User=default for dev.test.example.com (negated), got %q", val)
	}
}

func TestMatchAll(t *testing.T) {
	us := &UserSettings{
		userConfigFinder:   testConfigFinder("testdata/match-all"),
		systemConfigFinder: nullConfigFinder,
	}

	// "special" matches the explicit Host block first
	val := us.Get("special", "Port")
	if val != "1111" {
		t.Errorf("expected Port=1111 for special, got %q", val)
	}

	// "special" should also get User from the Match all block
	val = us.Get("special", "User")
	if val != "matchuser" {
		t.Errorf("expected User=matchuser for special, got %q", val)
	}

	// An arbitrary host should match "Match all"
	val = us.Get("anything.example.com", "Port")
	if val != "4567" {
		t.Errorf("expected Port=4567 for anything.example.com via Match all, got %q", val)
	}
	val = us.Get("anything.example.com", "User")
	if val != "matchuser" {
		t.Errorf("expected User=matchuser for anything.example.com via Match all, got %q", val)
	}
}

func TestMatchMixed(t *testing.T) {
	us := &UserSettings{
		userConfigFinder:   testConfigFinder("testdata/match-mixed"),
		systemConfigFinder: nullConfigFinder,
	}

	// "bastion" matches the explicit Host block
	val := us.Get("bastion", "Port")
	if val != "22" {
		t.Errorf("expected Port=22 for bastion, got %q", val)
	}
	val = us.Get("bastion", "User")
	if val != "root" {
		t.Errorf("expected User=root for bastion, got %q", val)
	}

	// app.prod.example.com matches "Match Host *.prod.example.com" and
	// also "Host *.example.com". First match wins for each key.
	val = us.Get("app.prod.example.com", "Port")
	if val != "2222" {
		t.Errorf("expected Port=2222 for app.prod.example.com, got %q", val)
	}
	val = us.Get("app.prod.example.com", "User")
	if val != "deploy" {
		t.Errorf("expected User=deploy for app.prod.example.com, got %q", val)
	}

	// app.staging.example.com matches "Host *.example.com" (Port 80)
	// first, then "Match Host *.staging.example.com" (Port 3333).
	// SSH semantics: first match wins per key.
	val = us.Get("app.staging.example.com", "Port")
	if val != "80" {
		t.Errorf("expected Port=80 for app.staging.example.com (Host block first), got %q", val)
	}

	// plain.example.com matches "Host *.example.com" only
	val = us.Get("plain.example.com", "Port")
	if val != "80" {
		t.Errorf("expected Port=80 for plain.example.com, got %q", val)
	}
	val = us.Get("plain.example.com", "User")
	if val != "webuser" {
		t.Errorf("expected User=webuser for plain.example.com, got %q", val)
	}

	// unknown host matches only "Match all"
	val = us.Get("unknown.host", "User")
	if val != "fallback" {
		t.Errorf("expected User=fallback for unknown.host via Match all, got %q", val)
	}
}

func TestMatchMixedGetAll(t *testing.T) {
	us := &UserSettings{
		userConfigFinder:   testConfigFinder("testdata/match-mixed"),
		systemConfigFinder: nullConfigFinder,
	}

	// app.prod.example.com should get both IdentityFiles from the Match Host
	// block, plus the one from Match all.
	vals := us.GetAll("app.prod.example.com", "IdentityFile")
	want := []string{"~/.ssh/prod_key1", "~/.ssh/prod_key2", "~/.ssh/default_key"}
	if len(vals) != len(want) {
		t.Fatalf("GetAll IdentityFile for app.prod.example.com: got %d values %v, want %d values %v", len(vals), vals, len(want), want)
	}
	for i := range want {
		if vals[i] != want[i] {
			t.Errorf("GetAll IdentityFile[%d]: got %q, want %q", i, vals[i], want[i])
		}
	}
}

func TestMatchDirectiveInline(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		alias   string
		key     string
		wantVal string
		wantErr string
	}{
		{
			name: "basic match host",
			config: `Match Host *.example.com
    Port 2222`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "match host no match",
			config: `Match Host *.example.com
    Port 2222`,
			alias:   "test.other.com",
			key:     "Port",
			wantVal: "",
		},
		{
			name: "match all",
			config: `Match all
    Port 9999`,
			alias:   "anything",
			key:     "Port",
			wantVal: "9999",
		},
		{
			name: "match host multiple patterns",
			config: `Match Host *.example.com *.example.org
    Port 2222`,
			alias:   "test.example.org",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "match host with comment",
			config: `Match Host *.example.com  # Production servers
    Port 2222`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "empty match should error",
			config: `Match
    Port 2222`,
			wantErr: "Match directive requires",
		},
		{
			name: "match host case insensitive",
			config: `Match HOST *.example.com
    Port 2222`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "match host mixed case",
			config: `Match HoSt *.example.com
    Port 2222`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "match all uppercase",
			config: `Match ALL
    Port 9999`,
			alias:   "anything",
			key:     "Port",
			wantVal: "9999",
		},
		{
			name: "match keyword itself case insensitive",
			config: `MATCH Host *.example.com
    Port 2222`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "match host extra spaces between patterns",
			config: `Match Host   *.example.com   *.example.org
    Port 2222`,
			alias:   "test.example.org",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name:    "match host trailing spaces",
			config:  "Match Host *.example.com   \n    Port 2222",
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "match host leading spaces on match line",
			config: `  Match Host *.example.com
    Port 2222`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222",
		},
		{
			name: "host before match host, same pattern",
			config: `Host *.example.com
    Port 1111

Match Host *.example.com
    Port 2222`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "1111", // Host block appears first, wins
		},
		{
			name: "match host before host, same pattern",
			config: `Match Host *.example.com
    Port 2222

Host *.example.com
    Port 1111`,
			alias:   "test.example.com",
			key:     "Port",
			wantVal: "2222", // Match block appears first, wins
		},
		{
			name: "match host provides key not in host",
			config: `Host *.example.com
    Port 1111

Match Host *.example.com
    User admin`,
			alias:   "test.example.com",
			key:     "User",
			wantVal: "admin", // Not set in Host, comes from Match
		},
		{
			name: "match host negation excludes",
			config: `Match Host *.example.com !staging.example.com
    Port 2222`,
			alias:   "staging.example.com",
			key:     "Port",
			wantVal: "",
		},
		{
			name: "match host negation allows",
			config: `Match Host *.example.com !staging.example.com
    Port 2222`,
			alias:   "prod.example.com",
			key:     "Port",
			wantVal: "2222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Decode(strings.NewReader(tt.config))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			got, err := cfg.Get(tt.alias, tt.key)
			if err != nil {
				t.Fatalf("unexpected Get error: %v", err)
			}
			if got != tt.wantVal {
				t.Errorf("Get(%q, %q) = %q, want %q", tt.alias, tt.key, got, tt.wantVal)
			}
		})
	}
}

func TestMatchUnsupportedCriteria(t *testing.T) {
	// Every Match criterion from the ssh_config manpage that we don't
	// support, plus case variations and the special Exec case.
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		// Exec gets its own error message because it's a security concern.
		{
			name:    "exec lowercase",
			config:  "Match exec \"echo hello\"\n    Port 22",
			wantErr: "ssh_config: Match Exec is not supported",
		},
		{
			name:    "exec uppercase",
			config:  "Match EXEC \"echo hello\"\n    Port 22",
			wantErr: "ssh_config: Match Exec is not supported",
		},
		{
			name:    "exec mixed case",
			config:  "Match ExEc \"echo hello\"\n    Port 22",
			wantErr: "ssh_config: Match Exec is not supported",
		},
		{
			name:    "exec with complex command",
			config:  "Match Exec \"test -f /etc/ssh/flag\"\n    Port 22",
			wantErr: "ssh_config: Match Exec is not supported",
		},
		// All other unsupported criteria.
		{
			name:    "user",
			config:  "Match User admin\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "user uppercase",
			config:  "Match USER admin\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "localuser",
			config:  "Match LocalUser kevin\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "localuser uppercase",
			config:  "Match LOCALUSER kevin\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "originalhost",
			config:  "Match OriginalHost *.example.com\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "originalhost uppercase",
			config:  "Match ORIGINALHOST *.example.com\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "canonical",
			config:  "Match canonical\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "final",
			config:  "Match final\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "tagged",
			config:  "Match Tagged mytag\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "localnetwork",
			config:  "Match LocalNetwork 192.168.1.0/24\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		{
			name:    "completely bogus criterion",
			config:  "Match Bogus value\n    Port 22",
			wantErr: "ssh_config: unsupported Match criterion",
		},
		// Match Host with no patterns after it.
		{
			name:    "match host with no patterns",
			config:  "Match Host\n    Port 22",
			wantErr: "ssh_config: Match Host requires at least one pattern",
		},
		// Match Host followed by only whitespace.
		{
			name:    "match host only whitespace after",
			config:  "Match Host   \n    Port 22",
			wantErr: "ssh_config: Match Host requires at least one pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tt.config))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestMatchDirectiveGetAll(t *testing.T) {
	config := `Match Host *.prod.example.com
    IdentityFile ~/.ssh/prod_key1
    IdentityFile ~/.ssh/prod_key2

Match all
    IdentityFile ~/.ssh/default_key`

	cfg, err := Decode(strings.NewReader(config))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	vals, err := cfg.GetAll("app.prod.example.com", "IdentityFile")
	if err != nil {
		t.Fatalf("unexpected GetAll error: %v", err)
	}
	want := []string{"~/.ssh/prod_key1", "~/.ssh/prod_key2", "~/.ssh/default_key"}
	if len(vals) != len(want) {
		t.Fatalf("GetAll returned %d values %v, want %d values %v", len(vals), vals, len(want), want)
	}
	for i := range want {
		if vals[i] != want[i] {
			t.Errorf("GetAll[%d] = %q, want %q", i, vals[i], want[i])
		}
	}
}

func TestMatchStringRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{
			name: "match host",
			config: `Match Host *.example.com
    Port 2222
`,
		},
		{
			name: "match all",
			config: `Match all
    Port 4567
`,
		},
		{
			name: "match host with comment",
			config: `Match Host *.example.com # production
    Port 2222
`,
		},
		{
			name: "match host multiple patterns",
			config: `Match Host *.example.com *.example.org
    Port 2222
`,
		},
		{
			name: "match ALL uppercase round-trip",
			config: `Match ALL
    Port 4567
`,
		},
		{
			name: "match All mixed case round-trip",
			config: `Match All
    Port 4567
`,
		},
		{
			name: "mixed host and match",
			config: `Host bastion
    Port 22

Match Host *.example.com
    Port 2222

Match all
    User fallback
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Decode(strings.NewReader(tt.config))
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			got := cfg.String()
			if got != tt.config {
				t.Errorf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", got, tt.config)
			}
		})
	}
}

func TestMatchFileRoundTrip(t *testing.T) {
	for _, filename := range []string{
		"testdata/match-host",
		"testdata/match-all",
		"testdata/match-mixed",
		"testdata/match-host-negation",
	} {
		data := loadFile(t, filename)
		cfg, err := Decode(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("%s: unexpected parse error: %v", filename, err)
		}
		got := cfg.String()
		if got != string(data) {
			t.Errorf("%s: round-trip mismatch:\ngot:\n%s\nwant:\n%s", filename, got, string(data))
		}
	}
}

// TestMatchExistingDirectiveFile tests that the existing testdata/match-directive
// file (which contains "Match all") now parses successfully.
func TestMatchExistingDirectiveFile(t *testing.T) {
	us := &UserSettings{
		userConfigFinder: testConfigFinder("testdata/match-directive"),
	}
	val := us.Get("anyhost", "Port")
	if val != "4567" {
		t.Errorf("expected Port=4567 via Match all, got %q", val)
	}
}
