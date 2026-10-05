package shellrun

import "testing"

func TestOperativeCommand(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command string
		want    string
		cut     bool
	}{
		{"no prefix", "go test ./...", "go test ./...", false},
		{"one assignment", "CGO_ENABLED=0 go build ./...", "go build ./...", true},
		{"several", "GOOS=linux GOARCH=arm64 go build ./cmd/reasonix", "go build ./cmd/reasonix", true},
		{
			"substitution holding a pipeline",
			"PATH=$(echo $PATH | tr ':' '\\n' | grep -v emsdk | paste -sd: -) go test ./internal/runtime/agent/",
			"go test ./internal/runtime/agent/",
			true,
		},
		{"quoting survives", `MSG="a b" printf '%s\n' "$MSG"`, `printf '%s\n' "$MSG"`, true},
		{"assignment only", "FOO=bar", "FOO=bar", false},
		{"export is a command, not a prefix", "export FOO=bar", "export FOO=bar", false},
		{"assignment later in a list is not a prefix", "go build ./... && CGO_ENABLED=0 go test ./...", "go build ./... && CGO_ENABLED=0 go test ./...", false},
		{"unparseable", "go test ./... && (", "go test ./... && (", false},
		{"empty", "", "", false},
		{"pipeline keeps its tail", "FOO=1 go test ./... | tee out.txt", "go test ./... | tee out.txt", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, cut := OperativeCommand(tc.command)
			if got != tc.want || cut != tc.cut {
				t.Fatalf("OperativeCommand(%q) = %q, %v; want %q, %v", tc.command, got, cut, tc.want, tc.cut)
			}
		})
	}
}
