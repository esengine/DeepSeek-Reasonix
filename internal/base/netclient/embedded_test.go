package netclient

import (
	"net"
	"testing"
)

func TestEmbeddedIPv4NamesTheAddressATransitionFormCarries(t *testing.T) {
	for in, want := range map[string]string{
		"64:ff9b::a9fe:a9fe": "169.254.169.254",
		"64:ff9b::7f00:1":    "127.0.0.1",
		"2002:a9fe:a9fe::1":  "169.254.169.254",
		"2002:7f00:1::":      "127.0.0.1",
		"::7f00:1":           "127.0.0.1",
		"::ffff:10.0.0.1":    "10.0.0.1",
	} {
		got := EmbeddedIPv4(net.ParseIP(in))
		if got == nil || !got.Equal(net.ParseIP(want)) {
			t.Errorf("EmbeddedIPv4(%s) = %v, want %s", in, got, want)
		}
	}
	for _, in := range []string{"2001:4860:4860::8888", "fe80::1", "2606:4700::1111"} {
		if got := EmbeddedIPv4(net.ParseIP(in)); got != nil {
			t.Errorf("EmbeddedIPv4(%s) = %v, want none", in, got)
		}
	}
}
