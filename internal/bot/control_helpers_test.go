package bot

import (
	"strings"
	"testing"
)

func TestValidateLoopbackAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		// Valid loopback addresses.
		{name: "ipv4 loopback", addr: "127.0.0.1:8080"},
		{name: "ipv4 loopback other octet", addr: "127.0.0.2:8080"},
		{name: "ipv6 loopback", addr: "[::1]:8080"},
		{name: "ipv6 loopback zero port", addr: "[::1]:0"},
		{name: "localhost", addr: "localhost:8080"},
		{name: "localhost case insensitive", addr: "LoCaLhOsT:9999"},
		// The implementation splits host:port only; a non-numeric port still
		// passes as long as the host is loopback.
		{name: "non-numeric port on loopback", addr: "127.0.0.1:notaport"},
		{name: "empty port on loopback", addr: "127.0.0.1:"},

		// Invalid: not loopback.
		{name: "wildcard", addr: "0.0.0.0:8080", wantErr: true},
		{name: "private ipv4", addr: "192.168.1.1:8080", wantErr: true},
		{name: "public ipv4", addr: "8.8.8.8:53", wantErr: true},
		{name: "non-loopback ipv6", addr: "[::2]:8080", wantErr: true},
		{name: "hostname other than localhost", addr: "example.com:8080", wantErr: true},

		// Invalid: malformed host:port.
		{name: "missing port ipv4", addr: "127.0.0.1", wantErr: true},
		{name: "missing port ipv6", addr: "[::1]", wantErr: true},
		{name: "missing port localhost", addr: "localhost", wantErr: true},
		{name: "empty address", addr: "", wantErr: true},
		{name: "unbracketed ipv6", addr: "::1:8080", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLoopbackAddr(tt.addr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateLoopbackAddr(%q) error = nil, want error", tt.addr)
				}
				return
			}
			if err != nil {
				t.Errorf("validateLoopbackAddr(%q) error = %v, want nil", tt.addr, err)
			}
		})
	}
}

func TestValidateLoopbackAddrErrorMentionsLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", "example.com:8080", "10.1.2.3:80"} {
		err := validateLoopbackAddr(addr)
		if err == nil {
			t.Errorf("validateLoopbackAddr(%q) error = nil, want error", addr)
			continue
		}
		if !strings.Contains(err.Error(), "loopback") {
			t.Errorf("validateLoopbackAddr(%q) error = %q, want loopback mention", addr, err)
		}
	}
}

func TestPrometheusLabelValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "plain", want: "plain"},
		{name: "empty", value: "", want: ""},
		{name: "double quote", value: `a"b`, want: `a\"b`},
		{name: "newline", value: "line\nbreak", want: `line\nbreak`},
		{name: "backslash", value: `back\slash`, want: `back\\slash`},
		{name: "all specials combined", value: `a"b\nc\d`, want: `a\"b\\nc\\d`},
		// Backslash is escaped before newline handling, so a literal
		// backslash-n input must not become a newline escape.
		{name: "literal backslash n not newline", value: `\n`, want: `\\n`},
		{name: "tab untouched", value: "tab\there", want: "tab\there"},
		{name: "unicode untouched", value: "日本語", want: "日本語"},
		{name: "trailing newlines", value: "multi\nline\n", want: `multi\nline\n`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prometheusLabelValue(tt.value); got != tt.want {
				t.Errorf("prometheusLabelValue(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestPrometheusLabelValueRemovesRawControlChars(t *testing.T) {
	for _, value := range []string{"line\nbreak", `a"b`, "both\n\"chars"} {
		got := prometheusLabelValue(value)
		if strings.Contains(got, "\n") {
			t.Errorf("prometheusLabelValue(%q) = %q, contains raw newline", value, got)
		}
	}
}
