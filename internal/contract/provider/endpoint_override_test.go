package provider

import "testing"

func TestEndpointOverrideRepeatsBase(t *testing.T) {
	const base = "http://127.0.0.1:8000/v1"
	cases := []struct {
		name     string
		override string
		bases    []string
		want     bool
	}{
		{"identical", base, []string{base}, true},
		{"trailing slash", base + "/", []string{base}, true},
		{"base with trailing slash", base, []string{base + "/"}, true},
		{"surrounding space", "  " + base + " ", []string{base}, true},
		{"scheme and host case", "HTTP://LocalHost:8000/v1", []string{"http://localhost:8000/v1"}, true},
		{"second base", "https://api.example.com/v1", []string{"https://api.example.com", "https://api.example.com/v1"}, true},
		{"path case differs", "http://127.0.0.1:8000/V1", []string{base}, false},
		{"query", base + "?token=1", []string{base}, false},
		{"empty query", base + "?", []string{base}, false},
		{"fragment", base + "#debug", []string{base}, false},
		{"deeper path", base + "/chat/completions", []string{base}, false},
		{"other host", "http://127.0.0.2:8000/v1", []string{base}, false},
		{"other port", "http://127.0.0.1:8001/v1", []string{base}, false},
		{"other scheme", "https://127.0.0.1:8000/v1", []string{base}, false},
		{"empty override", "", []string{base}, false},
		{"relative override", "127.0.0.1:8000/v1", []string{base}, false},
		{"no bases", base, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EndpointOverrideRepeatsBase(tc.override, tc.bases...); got != tc.want {
				t.Fatalf("EndpointOverrideRepeatsBase(%q, %q) = %v, want %v", tc.override, tc.bases, got, tc.want)
			}
		})
	}
}
