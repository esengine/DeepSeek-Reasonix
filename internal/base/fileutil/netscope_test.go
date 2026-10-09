package fileutil

import (
	"errors"
	"runtime"
	"testing"
)

func TestNetworkScopeOn(t *testing.T) {
	local := []string{`C:\work\proj`}
	unc := []string{`C:\work\proj`, `\\fileserver\team\proj`}
	cases := []struct {
		name  string
		path  string
		roots []string
		want  bool
	}{
		{"local path, local roots", `C:\work\proj\a.go`, local, true},
		{"local path outside roots is not this check's business", `D:\other\a.go`, local, true},
		{"relative path", `src\a.go`, local, true},
		{"bare drive", `C:`, local, true},
		{"drive relative", `C:a.go`, local, true},
		{"extended local", `\\?\C:\work\proj\a.go`, local, true},
		{"device drive", `\\.\C:\work`, local, true},

		{"unc with local roots", `\\evil\share\x`, local, false},
		{"slashes", `//evil/share/x`, local, false},
		{"mixed separators", `/\evil\share/x`, local, false},
		{"three slashes", `///evil/share/x`, local, false},
		{"extended unc", `\\?\UNC\evil\share\x`, local, false},
		{"extended unc lower", `\\?\unc\evil\share\x`, local, false},
		{"nt namespace unc", `\??\UNC\evil\share\x`, local, false},
		{"device unc", `\\.\UNC\evil\share\x`, local, false},
		{"pipe", `\\.\pipe\x`, local, false},
		{"global root", `\\?\GLOBALROOT\Device\x`, local, false},
		{"loopback admin share", `\\LOCALHOST\C$\Windows\win.ini`, local, false},
		{"loopback address", `\\127.0.0.1\C$`, local, false},
		{"host only", `\\evil`, local, false},
		{"trailing dot host", `\\evil.\share\x`, local, false},
		{"no roots", `\\evil\share\x`, nil, false},

		{"inside the unc root", `\\fileserver\team\proj\a.go`, unc, true},
		{"the unc root itself", `\\fileserver\team\proj`, unc, true},
		{"root with trailing separator", `\\fileserver\team\proj\`, unc, true},
		{"case differs", `\\FILESERVER\Team\PROJ\A.go`, unc, true},
		{"forward slashes", `//fileserver/team/proj/a.go`, unc, true},
		{"extended spelling of the root", `\\?\UNC\fileserver\team\proj\a.go`, unc, true},
		{"doubled separators", `\\fileserver\team\\proj\\a.go`, unc, true},
		{"dot segments inside", `\\fileserver\team\proj\.\sub\..\a.go`, unc, true},
		{"trailing dots on a component", `\\fileserver\team\proj.\a.go`, unc, true},
		{"trailing space on a component", `\\fileserver\team\proj \a.go`, unc, true},

		{"sibling share folder", `\\fileserver\team\proj2\a.go`, unc, false},
		{"parent of the root", `\\fileserver\team`, unc, false},
		{"dot dot out of the root", `\\fileserver\team\proj\..\other\a.go`, unc, false},
		{"dot dot out of the share", `\\fileserver\team\proj\..\..\..\x`, unc, false},
		{"other host same share", `\\evil\team\proj\a.go`, unc, false},
		{"host suffix", `\\fileserver.evil\team\proj\a.go`, unc, false},
		{"all-dots component", `\\fileserver\team\proj\...\a.go`, unc, false},
		{"three leading separators", `\\\fileserver\team\proj\a.go`, unc, false},
		{"unc root is not a prefix of a local path with same text", `\\fileserver\team\pro`, unc, false},
		{"unc under local-only roots", `\\fileserver\team\proj\a.go`, local, false},
		{"root that is only a host", `\\fileserver\x`, []string{`\\fileserver`}, false},
		{"root that is a device path", `\\.\pipe\x`, []string{`\\.\pipe`}, false},
	}
	for _, tc := range cases {
		err := NetworkScopeOn(true, tc.path, tc.roots)
		if (err == nil) != tc.want {
			t.Errorf("%s: NetworkScopeOn(%q, %q) = %v, want allowed=%v", tc.name, tc.path, tc.roots, err, tc.want)
		}
		if err != nil && !errors.Is(err, ErrNetworkPathOutsideScope) {
			t.Errorf("%s: refusal %v does not carry ErrNetworkPathOutsideScope", tc.name, err)
		}
	}
}

func TestNetworkScopeOnPosixIsNotAWindowsReading(t *testing.T) {
	if err := NetworkScopeOn(false, `//evil/share/x`, nil); err != nil {
		t.Fatalf("`//x` is a local path off Windows, got %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := NetworkScope(`//evil/share/x`, nil); err != nil {
			t.Fatalf("NetworkScope must not apply Windows rules on %s: %v", runtime.GOOS, err)
		}
	}
}
