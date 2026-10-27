package serve

import (
	"testing"

	"reasonix/internal/contract/config"
)

func TestServeWithoutAuthRefusesANetworkAddressUnlessAllowed(t *testing.T) {
	for _, c := range []struct {
		mode, addr string
		allow      bool
		refused    bool
	}{
		{"none", "0.0.0.0:8787", false, true},
		{"none", "192.168.1.5:8787", false, true},
		{"none", "[::]:8787", false, true},
		{"none", "0.0.0.0:8787", true, false},
		{"none", "127.0.0.1:8787", false, false},
		{"none", "localhost:8787", false, false},
		{"token", "0.0.0.0:8787", false, false},
		{"password", "0.0.0.0:8787", false, false},
	} {
		err := OpenBindRefusal(config.ServeConfig{AuthMode: c.mode}, c.addr, c.allow)
		if (err != nil) != c.refused {
			t.Errorf("%s on %s allow=%v: err=%v, want refused=%v", c.mode, c.addr, c.allow, err, c.refused)
		}
	}
}
