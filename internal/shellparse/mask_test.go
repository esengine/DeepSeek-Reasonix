package shellparse

import "testing"

func TestCanMaskEarlierFailure(t *testing.T) {
	tests := []struct {
		command string
		canMask bool
		ok      bool
	}{
		// `&&` short-circuits: bash reports the first failing command's status.
		{command: `go build ./... && go test ./...`, canMask: false, ok: true},
		{command: `a && b && c`, canMask: false, ok: true},
		{command: `go test ./...`, canMask: false, ok: true},
		{command: ``, canMask: false, ok: true},

		// Everything else lets a later command decide the exit status.
		{command: `go build ./... ; go test ./...`, canMask: true, ok: true},
		{command: "go build ./...\ngo test ./...", canMask: true, ok: true},
		{command: `go build ./... || true`, canMask: true, ok: true},
		{command: `go test ./... | tee out.txt`, canMask: true, ok: true},
		{command: `go test ./... |& tee out.txt`, canMask: true, ok: true},
		{command: `sleep 5 &`, canMask: true, ok: true},
		// A masking operator anywhere in the chain is enough.
		{command: `a && b ; c`, canMask: true, ok: true},
		{command: `a ; b && c`, canMask: true, ok: true},
		{command: `a && b || c`, canMask: true, ok: true},

		// Unanalyzable input must report ok=false rather than guess.
		{command: `if true; then go test ./...; fi`, canMask: false, ok: false},
		{command: `go test ./... &&`, canMask: false, ok: false},
		{command: "cat <<EOF\nx\nEOF", canMask: false, ok: false},
	}
	for _, tt := range tests {
		canMask, ok := CanMaskEarlierFailure(tt.command)
		if canMask != tt.canMask || ok != tt.ok {
			t.Errorf("CanMaskEarlierFailure(%q) = (%v, %v), want (%v, %v)",
				tt.command, canMask, ok, tt.canMask, tt.ok)
		}
	}
}
