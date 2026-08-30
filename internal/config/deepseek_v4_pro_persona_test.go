package config

import "testing"

func TestAppliesOfficialDeepSeekV4ProPersona(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry *ProviderEntry
		want  bool
	}{
		{
			name:  "nil",
			entry: nil,
			want:  false,
		},
		{
			name: "official pro",
			entry: &ProviderEntry{
				Name:    "deepseek",
				BaseURL: "https://api.deepseek.com",
				Model:   "deepseek-v4-pro",
			},
			want: true,
		},
		{
			name: "official anthropic pro",
			entry: &ProviderEntry{
				Name:    "deepseek",
				Kind:    "anthropic",
				BaseURL: "https://api.deepseek.com/anthropic",
				Model:   "deepseek-v4-pro",
			},
			want: true,
		},
		{
			name: "official flash",
			entry: &ProviderEntry{
				Name:    "deepseek",
				BaseURL: "https://api.deepseek.com",
				Model:   "deepseek-v4-flash",
			},
			want: false,
		},
		{
			name: "third party pro name",
			entry: &ProviderEntry{
				Name:    "novita",
				BaseURL: "https://api.novita.ai/openai",
				Model:   "deepseek/deepseek-v4-pro",
			},
			want: false,
		},
		{
			name: "empty model on official host",
			entry: &ProviderEntry{
				Name:    "deepseek",
				BaseURL: "https://api.deepseek.com",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AppliesOfficialDeepSeekV4ProPersona(tt.entry); got != tt.want {
				t.Fatalf("AppliesOfficialDeepSeekV4ProPersona() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyOfficialDeepSeekV4ProPersonaPrependsOnce(t *testing.T) {
	t.Parallel()
	pro := &ProviderEntry{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"}
	got := ApplyOfficialDeepSeekV4ProPersona("You are Reasonix, a coding agent.", pro)
	want := OfficialDeepSeekV4ProPersona + "\n\nYou are Reasonix, a coding agent."
	if got != want {
		t.Fatalf("prepend = %q, want %q", got, want)
	}
	if again := ApplyOfficialDeepSeekV4ProPersona(got, pro); again != want {
		t.Fatalf("second apply duplicated persona:\n%s", again)
	}
}

func TestApplyOfficialDeepSeekV4ProPersonaStripsForOtherModels(t *testing.T) {
	t.Parallel()
	pro := &ProviderEntry{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"}
	flash := &ProviderEntry{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}
	withPersona := ApplyOfficialDeepSeekV4ProPersona("BASE", pro)
	got := ApplyOfficialDeepSeekV4ProPersona(withPersona, flash)
	if got != "BASE" {
		t.Fatalf("flash should drop the official pro persona, got %q", got)
	}
}

func TestReasoningLanguageForEntryPinsOfficialProAutoToEnglish(t *testing.T) {
	t.Parallel()
	pro := &ProviderEntry{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"}
	flash := &ProviderEntry{BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}
	if got := ReasoningLanguageForEntry(pro, "auto"); got != "en" {
		t.Fatalf("official pro auto = %q, want en", got)
	}
	if got := ReasoningLanguageForEntry(pro, "zh"); got != "zh" {
		t.Fatalf("explicit zh must stay user-owned, got %q", got)
	}
	if got := ReasoningLanguageForEntry(flash, "auto"); got != "auto" {
		t.Fatalf("flash auto = %q, want auto", got)
	}
}
