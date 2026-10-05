package remote

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestHostKeyMismatchErrorKeepsStructuredSecurityDetails(t *testing.T) {
	err := newHostKeyMismatchError("dev@example.test:2222", "SHA256:new", &knownhosts.KeyError{
		Want: []knownhosts.KnownKey{
			{Filename: "/home/dev/.ssh/known_hosts", Line: 7},
			{Filename: "/etc/ssh/ssh_known_hosts", Line: 3},
		},
	})
	if !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("error does not unwrap to ErrHostKeyMismatch: %v", err)
	}
	var mismatch *HostKeyMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error is not HostKeyMismatchError: %T", err)
	}
	if mismatch.PresentedFingerprint != "SHA256:new" || len(mismatch.Locations) != 2 {
		t.Fatalf("structured mismatch details = %+v", mismatch)
	}
	if mismatch.Locations[0].Filename != "/home/dev/.ssh/known_hosts" || mismatch.Locations[0].Line != 7 {
		t.Fatalf("first known_hosts record = %+v", mismatch.Locations[0])
	}
	for _, want := range []string{"remote: host key mismatch", "SHA256:new", "/home/dev/.ssh/known_hosts:7"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("legacy error text %q missing %q", err, want)
		}
	}
}
