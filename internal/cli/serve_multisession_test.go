package cli

import (
	"flag"
	"strings"
	"testing"

	"reasonix/internal/remote/bootstrap"
)

func TestServeCapabilityHelpAdvertisesBootstrapProbeToken(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	registerServeCapabilityFlags(fs)
	usage := fs.Lookup("session-events").Usage
	if !strings.Contains(usage, bootstrap.ServeCapsToken) {
		t.Fatalf("session-events help = %q, want capability token %q", usage, bootstrap.ServeCapsToken)
	}
}
