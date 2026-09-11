package provider

import "testing"

func TestIsOfficialDeepSeekImageModel(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  bool
	}{
		{"deepseek-flash", true},
		{"DEEPSEEK-FLASH", true},
		{" deepseek-flash ", true},
		{"deepseek-v4.1-flash-expires-on-0910", true},
		{"deepseek-v4-flash", true},
		{OfficialDeepSeekVisionModel, true},
		{"deepseek-v4-pro", false},
		{"deepseek-v4-flash-0731", false},
		{"deepseek-v5-flash", false},
		{"", false},
	} {
		if got := IsOfficialDeepSeekImageModel(tc.model); got != tc.want {
			t.Errorf("IsOfficialDeepSeekImageModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestIsOfficialDeepSeekTextModel(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  bool
	}{
		{"deepseek-v4-pro", true},
		{" DEEPSEEK-V4-PRO ", true},
		// V4.1 Flash serves these two under the vendor's transition aliases, so
		// they must stay out of the text-only block.
		{"deepseek-v4-flash", false},
		{"deepseek-flash", false},
		{"deepseek-v5-flash", false},
		{"", false},
	} {
		if got := IsOfficialDeepSeekTextModel(tc.model); got != tc.want {
			t.Errorf("IsOfficialDeepSeekTextModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}
