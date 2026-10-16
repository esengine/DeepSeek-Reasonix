package netdns

import (
	"net"
	"os"
	"reflect"
	"testing"
)

// The parse is the part that runs on a machine that is not a phone, so it is
// the part worth pinning here: everything beyond it is a `net.Resolver` the
// standard library already tests.
func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "  \t\n ", nil},
		{"one v4", "10.0.0.1", []string{"10.0.0.1:53"}},
		{"comma separated", "10.0.0.1,8.8.8.8", []string{"10.0.0.1:53", "8.8.8.8:53"}},
		{"spaces and semicolons", "10.0.0.1; 8.8.8.8", []string{"10.0.0.1:53", "8.8.8.8:53"}},
		{"explicit port", "10.0.0.1:5353", []string{"10.0.0.1:5353"}},
		{"one v6", "fd00::1", []string{"[fd00::1]:53"}},
		{"two v6", "fd00::1,fd00::2", []string{"[fd00::1]:53", "[fd00::2]:53"}},
		// A trailing zone is how Android reports a link-local server.
		{"v6 with zone", "fe80::1%wlan0", []string{"[fe80::1%wlan0]:53"}},
		{"mixed garbage", "10.0.0.1,,not-an-address,8.8.8.8", []string{"10.0.0.1:53", "8.8.8.8:53"}},
		{"newlines", "10.0.0.1\n8.8.8.8\n", []string{"10.0.0.1:53", "8.8.8.8:53"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// Install must be inert without the variable, or a desktop build or the test
// suite would replace a working system resolver with an empty one.
func TestInstallIsInertWithoutTheVariable(t *testing.T) {
	t.Setenv(EnvVar, "")
	before := net.DefaultResolver
	Install()
	if net.DefaultResolver != before {
		t.Fatal("Install replaced the resolver with no servers configured")
	}
}

// With servers named, Install takes over — that is the whole point, and a silent
// no-op here is the bug this package exists to fix.
func TestInstallTakesOverWhenServersAreNamed(t *testing.T) {
	t.Setenv(EnvVar, "10.0.0.1")
	before := net.DefaultResolver
	t.Cleanup(func() { net.DefaultResolver = before })
	Install()
	if net.DefaultResolver == before {
		t.Fatal("Install left the default resolver in place with servers configured")
	}
	if !net.DefaultResolver.PreferGo {
		t.Fatal("the installed resolver must prefer the Go path: this build has no cgo resolver")
	}
}

// A malformed variable must not take resolution down with it.
func TestParseDropsGarbage(t *testing.T) {
	if servers := Parse("this is not an address"); len(servers) != 0 {
		t.Fatalf("Parse kept %v, want none", servers)
	}
}

var _ = os.Getenv
