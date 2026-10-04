package config

import "testing"

func TestSkillNameRejectsDefaultIgnorableCharacters(t *testing.T) {
	for _, name := range []string{
		"code-review\uFE0F", // variation selector
		"code\u034Freview",  // combining grapheme joiner
		"\u3164",            // Hangul filler
		"\u180Bname",        // Mongolian free variation selector
		"\u17B4name",        // Khmer inherent vowel sign
		"\U000E0101name",    // supplemental variation selector
		"\u115Fname",        // Hangul choseong filler
		"\uFFA0",            // halfwidth Hangul filler
	} {
		if IsValidSkillName(name) || SkillNameKey(name) != "" {
			t.Errorf("default-ignorable skill name %q was accepted", name)
		}
	}
	if !IsValidSkillName("中文技能") || !IsValidSkillName("Cafe\u0301") {
		t.Fatal("ordinary Unicode skill names should remain valid")
	}
}

func TestResolveSkillName(t *testing.T) {
	for _, tc := range []struct{ stem, declared, want string }{
		{"review", "", "review"},
		{"review", "inspect", "inspect"},
		{"review", "审查", "review"},
		{"审查", "点検", "点検"},
		{"审查", "inspect", "inspect"},
		{"cafe\u0301", "", "café"},
		{"审查", "re\u0301vision", "révision"},
		{"review", "bad/name", "review"},
		{"review", " inspect ", "review"},
		{"review", "rev\u200diew", "review"},
		{"审查", "审\ufe0f", "审查"},
		{"审查", "\u0301review", "审查"},
	} {
		t.Run(tc.stem+"/"+tc.declared, func(t *testing.T) {
			if got := ResolveSkillName(tc.stem, tc.declared); got != tc.want {
				t.Fatalf("ResolveSkillName(%q, %q) = %q, want %q", tc.stem, tc.declared, got, tc.want)
			}
		})
	}
}
