package evidence

import "testing"

func TestFullVerificationCommandRejectsTargetedChecks(t *testing.T) {
	cases := map[string]bool{
		"go test ./...":                   true,
		"go vet ./...":                    true,
		"go test ./internal/auth":         false,
		"go test ./... -run TestLogin":    false,
		"git diff --check":                false,
		"pytest tests/":                   true,
		"pytest tests/test_login.py":      false,
		"npm test":                        true,
		"npm test -- auth":                false,
		"cargo test --all-features":       true,
		"cargo test --package auth":       false,
		"GOROOT=/x go test ./...":         true,
		"cd desktop && go test ./...":     true,
		"go test ./... 2>&1 | tail -3":    false,
		"go test ./... || true":           false,
		"go test ./...; true":             false,
		"go test ./internal/auth && true": false,
	}
	for command, want := range cases {
		if got := IsFullVerificationCommand(command); got != want {
			t.Errorf("IsFullVerificationCommand(%q) = %v, want %v", command, got, want)
		}
	}
}
