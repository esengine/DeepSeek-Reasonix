package installsource

import "testing"

func TestLooksLikePackageVersionPin(t *testing.T) {
	accept := []string{
		"@scope/name",
		"name",
		"@playwright/mcp@0.0.83",
		"name@1.2.3",
		"name@0.0.0",
		"@scope/name@10.20.30",
		"name@1.2.3-beta.1",
		"name@1.2.3-rc-1",
		"name@1.2.3+build.5",
		"name@1.2.3-alpha.1+sha.abc",
	}
	for _, s := range accept {
		if !LooksLikePackage(s) {
			t.Errorf("LooksLikePackage(%q) = false, want true", s)
		}
	}
	reject := []string{
		"name@",
		"@scope/name@",
		"name@latest",
		"name@next",
		"name@beta",
		"name@^1.2.3",
		"name@~1.2.3",
		"name@>=1.2.3",
		"name@>1.2.3",
		"name@<2",
		"name@1",
		"name@1.2",
		"name@1.2.x",
		"name@*",
		"name@1.2.3 - 2.0.0",
		"name@1.2.3||2.0.0",
		"name@01.2.3",
		"name@1.2.3-",
		"name@1.2.3+",
		"name@1.2.3-a..b",
		"name@v1.2.3",
		"a@b@c",
		"name@1.2.3@4.5.6",
		"@a@b/c@1.2.3",
		"@scope@1.2.3",
		"@scope/name/extra@1.2.3",
		"@@scope/name@1.2.3",
		"@/name@1.2.3",
		"@scope/@1.2.3",
		"@1.2.3",
		"@1.2.3@1.2.3",
		"name@1.2.3;rm",
		"name@1.2.3;rm -rf /",
		"name@1.2.3 --foo",
		"name @1.2.3",
		"name@ 1.2.3",
		"name@1.2.3\n",
		"name@1.2.3\t",
		"name@1.2.3/../x",
		"name@..",
		"name@../1.2.3",
		"../name@1.2.3",
		"./name@1.2.3",
		"/abs/name@1.2.3",
		"name@1.2.3\\x",
		"name@1.2.3$(id)",
		"name@1.2.3`id`",
		"name@1.2.3&x",
		"name@1.2.3|x",
		"name@1.2.3?x=1",
		"name@1.2.3#x",
		"name@1.2.3\x00",
		"na\x00me@1.2.3",
		"name@1.2.3\u00a0",
		"name@\uff11.2.3",
		"na\u043de@1.2.3",
		"name\u200b@1.2.3",
		"name@1.2.3\u2028",
		"file:name@1.2.3",
		"file:../x",
		"git+https://github.com/o/r.git@1.2.3",
		"git+ssh://git@github.com/o/r.git",
		"npm:name@1.2.3",
		"github:o/r@1.2.3",
		"o/r@1.2.3",
		"https:@1.2.3",
		"-y@1.2.3",
		"--help",
		"-y",
		"--package=x@1.2.3",
		"",
	}
	for _, s := range reject {
		if LooksLikePackage(s) {
			t.Errorf("LooksLikePackage(%q) = true, want false", s)
		}
	}
}

func TestPackageMCPActionPinnedVersion(t *testing.T) {
	tl := NewTool(Options{})
	for _, tc := range []struct{ source, name string }{
		{"@playwright/mcp@0.0.83", "playwright-mcp"},
		{"my-server@1.2.3", "my-server"},
		{"@scope/pkg", "scope-pkg"},
	} {
		a := tl.packageMCPAction(request{Source: tc.source, Kind: "mcp"})
		if len(a.Args) != 2 || a.Args[0] != "-y" || a.Args[1] != tc.source {
			t.Errorf("%s: args = %v, want [-y %s]", tc.source, a.Args, tc.source)
		}
		if a.Name != tc.name {
			t.Errorf("%s: name = %q, want %q", tc.source, a.Name, tc.name)
		}
	}
}

func TestLooksLikePackageRejectsDotOnlySegments(t *testing.T) {
	for _, s := range []string{"@a/..", "@../b", "@./b", "@a/.", "@../..", "@a/..@1.2.3"} {
		if LooksLikePackage(s) {
			t.Errorf("LooksLikePackage(%q) = true, want false", s)
		}
	}
	for _, s := range []string{"@a/b.c", "@a.b/c", "@a/b..c", "@a/..b", "@a/b.."} {
		if !LooksLikePackage(s) {
			t.Errorf("LooksLikePackage(%q) = false, want true", s)
		}
	}
}
