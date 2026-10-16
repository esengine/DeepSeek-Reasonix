package boot

import (
	"runtime"
	"testing"
)

// A host that ships no browser binary and names none must not be handed the
// browser tools: every call would end in browser.engine_missing, which asks the
// reader to install a browser the platform cannot install. Android is that
// host — it has a WebView, which is not a process this pool can launch.
func TestBrowserWithoutEngineOnAndroid(t *testing.T) {
	if !browserWithoutEngine("android", "") {
		t.Fatal("android with no configured executable should have no browser tools")
	}
	// A configured executable is taken at its word: the host that says where
	// its browser is has one, and the pool decides the rest at first use.
	if browserWithoutEngine("android", "/data/local/tmp/chromium") {
		t.Fatal("a configured executable should keep the browser tools on android")
	}
	// Whitespace is not a path.
	if !browserWithoutEngine("android", "   ") {
		t.Fatal("a blank executable is the same as none")
	}
	// Every other platform keeps the existing behavior: the tools are bound and
	// the failure, if any, is discovered when the pool is first used.
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if browserWithoutEngine(goos, "") {
			t.Fatalf("%s should keep its browser tools", goos)
		}
	}
}

// The check is on the platform, not on the machine running the test.
func TestBrowserWithoutEngineIgnoresThisHost(t *testing.T) {
	if browserWithoutEngine(runtime.GOOS, "") && runtime.GOOS != "android" {
		t.Fatalf("this host (%s) has no engine but should still bind the tools", runtime.GOOS)
	}
}
