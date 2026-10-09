package fileutil

import "testing"

func TestIsNetworkPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`\\host\share\x`, true},
		{`//host/share/x`, true},
		{`\/host/share`, true},
		{`/\host\share`, true},
		{`\\host`, true},
		{`\\127.0.0.1\c$\windows`, true},
		{`\\?\UNC\host\share\x`, true},
		{`\\?\unc\host\share\x`, true},
		{`//?/UNC/host/share/x`, true},
		{`\\.\UNC\host\share\x`, true},
		{`\\.\pipe\name`, true},
		{`\\?\GLOBALROOT\Device\x`, true},
		{`\\?\Volume{0000}\x`, true},
		{`\??\UNC\host\share\x`, true},
		{`\??\unc\host\share\x`, true},
		{`///host/share`, true},
		{`\\`, true},

		{`C:\work\x`, false},
		{`c:/work/x`, false},
		{`\\?\C:\work\x`, false},
		{`\\.\C:\work\x`, false},
		{`\??\C:\work\x`, false},
		{`/home/me/x`, false},
		{`/`, false},
		{`\`, false},
		{`relative\x`, false},
		{`a//b`, false},
		{`C:\a\\b`, false},
		{``, false},
		{`x`, false},
	}
	for _, tc := range cases {
		if got := IsNetworkPath(tc.path); got != tc.want {
			t.Errorf("IsNetworkPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
