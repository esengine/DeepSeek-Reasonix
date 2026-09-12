package builtin

import (
	"errors"
	"testing"
)

func TestSandboxWriteDenialClassifierRejectsOrdinaryFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		err  error
	}{
		{name: "python key error", out: "Traceback\nKeyError: 'observation_time'", err: errors.New("exit status 1")},
		{name: "http failure", out: "HTTP 403: permission denied", err: errors.New("exit status 22")},
		{name: "timeout", out: "curl: (28) operation timed out", err: errors.New("exit status 28")},
		{name: "sandbox startup", out: "", err: errors.New("sandbox helper failed to start")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if looksLikeSandboxWriteDenial(tc.out, tc.err) {
				t.Fatal("ordinary failure was classified as a sandbox write denial")
			}
		})
	}
}

func TestSandboxWriteDenialClassifierAcceptsFilesystemFailures(t *testing.T) {
	for _, out := range []string{
		"touch: /outside/file: Permission denied",
		"bash: /outside/file: Permission denied",
		"mkdir: cannot create directory '/outside': Read-only file system",
		"open /outside/file: operation not permitted",
		"PermissionError: [Errno 13] Permission denied: '/outside/file'",
	} {
		if !looksLikeSandboxWriteDenial(out, errors.New("exit status 1")) {
			t.Fatalf("filesystem failure was not recognized: %q", out)
		}
	}
}
