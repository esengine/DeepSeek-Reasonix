//go:build windows

package wsl

import (
	"context"
	"os"
	"testing"
)

// REASONIX_LIVE_WSL names an installed distribution to probe for real.
func TestLiveDistributionIsListedAndProbed(t *testing.T) {
	name := os.Getenv("REASONIX_LIVE_WSL")
	if name == "" {
		t.Skip("set REASONIX_LIVE_WSL to an installed distribution")
	}
	ctx := context.Background()
	list, err := List(ctx, System())
	if err != nil || len(list) == 0 {
		t.Fatalf("List = %v, %v", list, err)
	}
	t.Logf("distributions: %+v", list)
	p, err := Check(ctx, System(), name)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("probe: %+v", p)
	if p.Arch == "" || p.User == "" || p.Distro.Version == 0 {
		t.Fatalf("probe = %+v", p)
	}
}
