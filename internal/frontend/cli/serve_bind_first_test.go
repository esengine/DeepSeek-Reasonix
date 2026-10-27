package cli

import (
	"net"
	"strings"
	"testing"
)

func TestServeRefusesAnAddressItCannotBindBeforeAnythingIsReported(t *testing.T) {
	for _, addr := range []string{"bad", "127.0.0.1:99999", "127.0.0.1:-1", ":notaport"} {
		t.Run(addr, func(t *testing.T) {
			resources, err := prepareServeFrontend(serveFrontendOptions{command: "serve", address: addr})
			if err == nil {
				resources.release(true)
				t.Fatalf("serve accepted %q: the banner, share link and token path would be printed for a server that never starts", addr)
			}
		})
	}
}

func TestServeReportsTheAddressItActuallyBound(t *testing.T) {
	resources, err := prepareServeFrontend(serveFrontendOptions{command: "serve", address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer resources.release(true)
	if resources.listener == nil {
		t.Fatal("serve reported before it listened")
	}
	_, port, err := net.SplitHostPort(resources.displayAddr)
	if err != nil || port == "0" || !strings.HasPrefix(resources.displayAddr, "127.0.0.1:") {
		t.Fatalf("displayAddr = %q, want the bound 127.0.0.1 port (%v)", resources.displayAddr, err)
	}
}

func TestServeBindsOnceAndTheSecondUseOfTheSamePortIsRefused(t *testing.T) {
	first, err := prepareServeFrontend(serveFrontendOptions{command: "serve", address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.release(true)
	second, err := prepareServeFrontend(serveFrontendOptions{command: "serve", address: first.displayAddr})
	if err == nil {
		second.release(true)
		t.Fatalf("a second serve bound %s while the first still holds it", first.displayAddr)
	}
}
