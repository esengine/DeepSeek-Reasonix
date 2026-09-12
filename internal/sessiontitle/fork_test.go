package sessiontitle

import "testing"

func TestIncreaseFork(t *testing.T) {
	tests := map[string]string{
		"":                             "",
		"Roadmap":                      "Roadmap (1)",
		"Roadmap (1)":                  "Roadmap (2)",
		"计划（1）":                        "计划（2）",
		"计划 （9）":                       "计划 （10）",
		"Archive (notes)":              "Archive (notes) (1)",
		"Huge (999999999999999999999)": "Huge (1000000000000000000000)",
	}
	for input, want := range tests {
		if got := IncreaseFork(input); got != want {
			t.Errorf("IncreaseFork(%q) = %q, want %q", input, got, want)
		}
	}
}
