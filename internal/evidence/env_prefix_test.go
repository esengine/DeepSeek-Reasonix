package evidence

import "testing"

func TestEnvPrefixVerification(t *testing.T) {
	cases := map[string]bool{
		"go test ./internal/evidence/":                   true,
		"go test ./internal/evidence/ -count=1":          true,
		"GOROOT=/x go test ./internal/evidence/":         true,
		"GOROOT=/x PATH=/y go test ./internal/evidence/": true,
		"env GOROOT=/x go test ./internal/evidence/":     true,
		"cd /tmp && go test ./internal/evidence/":        true,
		"go test ./internal/evidence/ 2>&1 | tail -3":    true,
		"GOROOT=/x rm -rf /":                             false,
		"GOROOT=/x":                                      false,
		"FOO=$(whoami) go test ./internal/evidence/":     false,
		"env -i go test ./internal/evidence/":            false,
		"CARGO_HOME=/y cargo test":                       true,
	}
	for c, want := range cases {
		if got := IsVerificationCommand(c); got != want {
			t.Errorf("IsVerificationCommand(%q) = %v, want %v", c, got, want)
		}
	}
}
