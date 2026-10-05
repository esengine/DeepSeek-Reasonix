package config

import "testing"

func TestIssue4038SkillNamePersistenceUsesNFC(t *testing.T) {
	cfg := &Config{}
	if err := cfg.SetSkillEnabled("cafe\u0301", false); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Skills.DisabledSkills; len(got) != 1 || got[0] != "café" {
		t.Fatalf("stored disabled skill = %q, want NFC café", got)
	}
	if !cfg.IsSkillDisabled("cafe\u0301") || !cfg.IsSkillDisabled("café") {
		t.Fatal("NFD and NFC spellings must refer to the same disabled skill")
	}
	if err := cfg.SetSkillEnabled("café", true); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Skills.DisabledSkills) != 0 {
		t.Fatalf("enabled skill remained disabled: %q", cfg.Skills.DisabledSkills)
	}
}
