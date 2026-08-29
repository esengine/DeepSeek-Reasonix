package installsource

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"reasonix/internal/config"
)

// --- plan.go helpers --------------------------------------------------------

func TestHasUnsafeGitHubSourceCharacters(t *testing.T) {
	cases := map[string]bool{
		"owner/repo":                       false,
		"https://github.com/a/b/tree/main": false,
		"":                                 false,
		"owner repo":                       true, // space
		"owner\nrepo":                      true, // newline
		"owner\trepo":                      true, // tab
		"owner\x00repo":                    true, // NUL control
		"owner\x1brepo":                    true, // ESC control
		"owner\u00A0repo":                  true, // non-breaking space is Unicode whitespace
		"日本語":                              false,
		"owner\u200Brepo":                  false, // zero-width space is neither space nor control (documented gap)
		"a\x7fb":                           true,  // DEL control
	}
	for in, want := range cases {
		if got := hasUnsafeGitHubSourceCharacters(in); got != want {
			t.Errorf("hasUnsafeGitHubSourceCharacters(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSkipSkillRepoDir(t *testing.T) {
	cases := map[string]bool{
		"":             true,
		"   ":          true, // trimmed
		".git":         true,
		".github":      true,
		".GitHub":      true, // case-insensitive
		"node_modules": true,
		"references":   true,
		"scripts":      true,
		"assets":       true,
		"docs":         false,
		"skills":       false,
		"src":          false,
		".hidden":      false,
		"scripts2":     false, // prefix match must not trigger
	}
	for in, want := range cases {
		if got := skipSkillRepoDir(in); got != want {
			t.Errorf("skipSkillRepoDir(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestJoinURLPath(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"a", "b", "c"}, "a/b/c"},
		{[]string{"a/", "/b/", "/c"}, "a/b/c"}, // leading/trailing slashes stripped per part
		{[]string{"/", "a", "/"}, "a"},         // root-only parts vanish
		{[]string{"a", "", "b"}, "a/b"},        // empty parts skipped
		{[]string{"", "", ""}, ""},
		{[]string{"/"}, ""},
		{[]string{"a//", "//b"}, "a/b"}, // repeated slashes trimmed
		{[]string{"https://api.github.com", "repos"}, "https://api.github.com/repos"}, // scheme kept, only edge slashes trimmed
	}
	for _, c := range cases {
		if got := joinURLPath(c.in...); got != c.want {
			t.Errorf("joinURLPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- names.go: URL detection ------------------------------------------------

func TestIsURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com":           true,
		"http://example.com/path?q=1#f": true,
		"HTTPS://example.com/x":         true, // scheme is case-insensitive
		"http://127.0.0.1:3000/mcp":     true,
		"ftp://example.com":             false, // scheme not http/https
		"mailto:a@b.com":                false,
		"file:///tmp/x":                 false,
		"example.com":                   false, // no scheme
		"//example.com/x":               false, // scheme-relative: host but no scheme
		"http://":                       false, // no host
		"http://exa mple.com":           false, // parse error
		"relative/path":                 false,
		"":                              false,
	}
	for in, want := range cases {
		if got := isURL(in); got != want {
			t.Errorf("isURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLooksLikeMarkdownURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/skills/alpha.md": true,
		"https://example.com/SKILL.MD":        true, // extension is case-insensitive
		"https://example.com/a.md?raw=1":      true,
		"alpha.md":                            true, // lenient: any parseable string with a .md path
		"https://example.com/dir/":            false,
		"https://example.com":                 false, // no path
		"https://example.com/a.mcp.json":      false,
		"https://example.com/a.md/extra":      false, // .md is not the final path segment
	}
	for in, want := range cases {
		if got := looksLikeMarkdownURL(in); got != want {
			t.Errorf("looksLikeMarkdownURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLooksLikeMCPJSONURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/.mcp.json":     true,
		"https://example.com/a/.MCP.JSON":   true, // case-insensitive
		"https://example.com/.mcp.json?x=1": true,
		"https://example.com/mcp.json":      false, // missing dot prefix
		"https://example.com/.mcp.json5":    false,
		"https://example.com/a.md":          false,
		"https://example.com/":              false,
	}
	for in, want := range cases {
		if got := looksLikeMCPJSONURL(in); got != want {
			t.Errorf("looksLikeMCPJSONURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLooksLikeRemoteMCPEndpoint(t *testing.T) {
	cases := map[string]bool{
		"https://mcp.example.com/mcp":         true,  // mcp. host prefix
		"https://mcp-stripe.example.com/x":    true,  // mcp- host prefix
		"https://foo.mcp.example.com/x":       true,  // .mcp. host segment
		"https://foo-mcp.example.com/x":       true,  // -mcp. host segment
		"https://example.com/mcp":             true,  // mcp in path
		"https://example.com/sse":             true,  // sse in path
		"https://api.example.com/v1/mcp?x":    true,  // case-insensitive path
		"https://MCP.Example.COM/x":           true,  // case-insensitive host
		"https://example.com/":                false, // no signal
		"https://example.com/skills/alpha.md": false,
		"https://example.com/v1/health":       false,
		"https://example.com/micro":           false, // "micro" is not "mcp"
		"not a url":                           false,
	}
	for in, want := range cases {
		if got := looksLikeRemoteMCPEndpoint(in); got != want {
			t.Errorf("looksLikeRemoteMCPEndpoint(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLooksLikePackage(t *testing.T) {
	cases := map[string]bool{
		"some-package": true,
		"some.package": true,
		"some_package": true,
		"12345":        true,
		"pkg-1.2.3":    true,
		"@scope/pkg":   true,
		"@5/pkg":       true, // digit scope is valid
		"has space":    false,
		"has\ttab":     false,
		"back\\slash":  false,
		".hidden":      false,
		"/absolute":    false,
		"a/b":          false, // unscoped names must not contain /
		"@scope/a/b":   false, // scoped names take exactly two segments
		"@scope/":      false, // empty name segment
		"@":            false,
		"pkg@1.0.0":    false, // version specifiers are not package names
		"":             false,
	}
	for in, want := range cases {
		if got := looksLikePackage(in); got != want {
			t.Errorf("looksLikePackage(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tool")
	writeFile(t, exe, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(exe, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !isExecutable(exe, info) {
		t.Error("regular file with 0755 should be executable")
	}

	plain := filepath.Join(dir, "plain")
	writeFile(t, plain, "data")
	info, err = os.Stat(plain)
	if err != nil {
		t.Fatal(err)
	}
	if isExecutable(plain, info) {
		t.Error("regular file with 0644 should not be executable")
	}

	sub := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(sub)
	if err != nil {
		t.Fatal(err)
	}
	if isExecutable(sub, info) {
		t.Error("directory must not count as an executable file")
	}

	if runtime.GOOS == "windows" {
		for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
			p := filepath.Join(dir, "server"+ext)
			writeFile(t, p, "data")
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if !isExecutable(p, info) {
				t.Errorf("windows file with %s extension should be executable", ext)
			}
		}
		p := filepath.Join(dir, "server.js")
		writeFile(t, p, "data")
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if isExecutable(p, info) {
			t.Error("windows file without a known extension should not be executable")
		}
		return
	}

	// POSIX: symlink targets are followed by os.Stat at call sites, but the
	// link itself (Lstat info) is not a regular file.
	target := filepath.Join(dir, "target")
	writeFile(t, target, "#!/bin/sh\n")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err == nil {
		if linfo, lerr := os.Lstat(link); lerr == nil && isExecutable(link, linfo) {
			t.Error("symlink itself must not count as executable")
		}
	}
}

// --- names.go: name derivation and normalization ----------------------------

func TestNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://example.com/skills/alpha.md": "alpha",
		"https://example.com/alpha.MD":        "alpha",
		"https://example.com/a.b.c.md":        "a.b.c",
		"https://example.com/My Skill.md":     "my-skill", // sanitized stem
		"https://example.com/skills/":         "skills",   // trailing slash: last segment wins
		"https://example.com/foo":             "foo",
		"https://example.com/":                "mcp", // no name component
		"https://example.com":                 "mcp",
		"https://example.com/_alpha.md":       "alpha", // leading non-alphanumeric dropped
		"http://exa%zzmple.com/x":             "skill", // parse error fallback
	}
	for in, want := range cases {
		if got := nameFromURL(in); got != want {
			t.Errorf("nameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeMode(t *testing.T) {
	cases := map[string]string{
		"copy":     "copy",
		" LINK ":   "link",
		"Register": "register",
		"":         "auto",
		"symlink":  "auto",
		"xyz":      "auto",
	}
	for in, want := range cases {
		if got := normalizeMode(in); got != want {
			t.Errorf("normalizeMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModeForSingleSkill(t *testing.T) {
	cases := map[string]string{
		"link":     "link",
		"":         "copy",
		"copy":     "copy",
		"register": "copy",
		"auto":     "copy",
	}
	for in, want := range cases {
		if got := modeForSingleSkill(in); got != want {
			t.Errorf("modeForSingleSkill(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluginTransport(t *testing.T) {
	cases := map[string]string{
		"":                "stdio",
		"stdio":           "stdio",
		"STDIO":           "stdio",
		"http":            "http",
		"HTTP":            "http",
		"streamable-http": "http",
		"sse":             "sse",
		"SSE":             "sse",
		"auto":            "stdio",
		"carrier-pigeon":  "stdio",
		"https":           "stdio", // only http/streamable-http/sse are recognized
	}
	for typ, want := range cases {
		if got := pluginTransport(config.PluginEntry{Type: typ}); got != want {
			t.Errorf("pluginTransport(type %q) = %q, want %q", typ, got, want)
		}
	}
}

// --- names.go: map/string helpers -------------------------------------------

func TestCleanMap(t *testing.T) {
	if got := cleanMap(nil); got != nil {
		t.Errorf("cleanMap(nil) = %v, want nil", got)
	}
	if got := cleanMap(map[string]string{}); got != nil {
		t.Errorf("cleanMap(empty) = %v, want nil", got)
	}
	if got := cleanMap(map[string]string{"": "v"}); got != nil {
		t.Errorf("cleanMap(only empty key) = %v, want nil", got)
	}
	if got := cleanMap(map[string]string{"   ": "v"}); got != nil {
		t.Errorf("cleanMap(only whitespace key) = %v, want nil", got)
	}
	if got := cleanMap(map[string]string{"a": "1", " b ": "2", "": "3"}); !reflect.DeepEqual(got, map[string]string{"a": "1", "b": "2"}) {
		t.Errorf("cleanMap(mixed) = %v, want trimmed keys with empties dropped", got)
	}
	if got := cleanMap(map[string]string{"a": "1"}); !reflect.DeepEqual(got, map[string]string{"a": "1"}) {
		t.Errorf("cleanMap(clean) = %v, want unchanged", got)
	}
}

func TestCollapseSpaces(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"single":    "single",
		"  a   b  ": "a b",
		"a\tb\nc":   "a b c",
		"\t \n":     "",
		"a\u00A0b":  "a b", // unicode whitespace collapsed too
	}
	for in, want := range cases {
		if got := collapseSpaces(in); got != want {
			t.Errorf("collapseSpaces(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{}, ""},
		{[]string{"", "  ", "\t"}, ""},
		{[]string{"x", ""}, "x"},
		{[]string{"", "b", "c"}, "b"},
		{[]string{"  x  ", "y"}, "x"}, // result is trimmed
		{[]string{"", "", "z"}, "z"},
	}
	for _, c := range cases {
		if got := firstNonEmpty(c.in...); got != c.want {
			t.Errorf("firstNonEmpty(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
