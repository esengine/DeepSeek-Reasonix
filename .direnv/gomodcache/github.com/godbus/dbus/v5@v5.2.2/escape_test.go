package dbus

import (
	"testing"
)

var escapeTestCases = []struct {
	in, out string
}{
	{in: "", out: ""},
	{in: "ABCDabcdZYXzyx01289", out: "ABCDabcdZYXzyx01289"},
	{in: `_-/\*`, out: `_-/\*`},
	{in: `=+:~!`, out: `%3d%2b%3a%7e%21`},
	{in: `space here`, out: `space%20here`},
	{in: `Привет`, out: `%d0%9f%d1%80%d0%b8%d0%b2%d0%b5%d1%82`},
	{in: `ჰეი`, out: `%e1%83%b0%e1%83%94%e1%83%98`},
	{in: `你好`, out: `%e4%bd%a0%e5%a5%bd`},
	{in: `こんにちは`, out: `%e3%81%93%e3%82%93%e3%81%ab%e3%81%a1%e3%81%af`},
}

// More real world examples for more fair benchmark.
var escapeBenchmarkCases = []struct {
	in, out string
}{
	{in: "/run/user/1000/bus", out: "/run/user/1000/bus"},
	{in: "/path/with/a/single space/bus", out: "/path/with/a/single%20space/bus"},
}

func TestEscapeBusAddressValue(t *testing.T) {
	for _, tc := range escapeTestCases {
		out := EscapeBusAddressValue(tc.in)
		if out != tc.out {
			t.Errorf("input: %q; want %q, got %q", tc.in, tc.out, out)
		}
		in, err := UnescapeBusAddressValue(out)
		if err != nil {
			t.Errorf("unescape error: %v", err)
		} else if in != tc.in {
			t.Errorf("unescape: want %q, got %q", tc.in, in)
		}
	}
}

func BenchmarkEscapeBusAddressValue(b *testing.B) {
	var out string

	for i := 0; i < b.N; i++ {
		for _, tc := range escapeBenchmarkCases {
			out = EscapeBusAddressValue(tc.in)
		}
	}
	b.Log("out:", out)
}
