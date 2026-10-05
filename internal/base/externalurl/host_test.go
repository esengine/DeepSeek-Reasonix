package externalurl

import "testing"

func TestBrowserHostnameAndLiteralLocalRisk(t *testing.T) {
	for _, tc := range []struct {
		url, host string
		local     bool
	}{
		{"https://EXAMPLE.com./x", "example.com.", false},
		{"https://bücher.example/", "xn--bcher-kva.example", false},
		{"https://０ｘ７ｆ.１/", "127.0.0.1", true},
		{"https://0x7f.1/", "127.0.0.1", true},
		{"https://0177.0.0.1/", "127.0.0.1", true},
		{"https://2130706433/", "127.0.0.1", true},
		{"https://127.1./", "127.0.0.1", true},
		{"https://10.1/", "10.0.0.1", true},
		{"https://172.16.1.2/", "172.16.1.2", true},
		{"https://172.15.1.2/", "172.15.1.2", false},
		{"https://192.168.1.2/", "192.168.1.2", true},
		{"https://169.254.1.2/", "169.254.1.2", true},
		{"https://8.8.8.8/", "8.8.8.8", false},
		{"https://0/", "0.0.0.0", true},
		{"https://[0:0:0:0:0:0:0:1]/", "[::1]", true},
		{"https://[::ffff:127.0.0.1]/", "[::ffff:7f00:1]", true},
		{"https://[::ffff:8.8.8.8]/", "[::ffff:808:808]", false},
		{"https://[fd00::1]/", "[fd00::1]", true},
		{"https://[fe80::1]/", "[fe80::1]", true},
		{"https://[2001:4860:4860::8888]/", "[2001:4860:4860::8888]", false},
		{"https://test.LOCALHOST./", "test.localhost.", true},
		{"https://printer.local/", "printer.local", true},
		{"https://foo_bar.example/", "foo_bar.example", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			host, local, err := Host(tc.url)
			if err != nil || host != tc.host || local != tc.local {
				t.Fatalf("Host = %q, %v, %v; want %q, %v", host, local, err, tc.host, tc.local)
			}
		})
	}
}

func TestInvalidBrowserHosts(t *testing.T) {
	for _, raw := range []string{"https://", "https://09/", "https://256.1/", "https://4294967296/", "https://1.2.3.4.5/", "https://foo.1/", "https://[fe80::1%25en0]/", "file:///secret"} {
		if _, _, err := Host(raw); err == nil {
			t.Fatalf("accepted invalid URL %q", raw)
		}
	}
}
