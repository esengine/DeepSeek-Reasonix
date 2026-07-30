package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/mcplaunch"
)

func TestStoredNPXLauncherLockUsesExactOfflinePackage(t *testing.T) {
	manager := mcplaunch.NewManager(filepath.Join(t.TempDir(), mcplaunch.StateFilename), "/workspace")
	lock := mcplaunch.LauncherLock{
		Server: "search", Locator: digestText("@scope/server"), ResolvedVersion: "@scope/server@1.2.3", ContentSHA256: digestText("integrity"),
	}
	if err := manager.PutLauncherLock(lock); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Name: "search", Command: "npx", Args: []string{"-y", "@scope/server", "--stdio"}, LaunchManager: manager}
	locked, err := applyStoredLauncherLock(spec)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-y", "--offline", "@scope/server@1.2.3", "--stdio"}
	if !reflect.DeepEqual(locked.LaunchArgs, want) {
		t.Fatalf("launch args = %v, want %v", locked.LaunchArgs, want)
	}
	if wantIdentity := []string{"-y", "@scope/server@1.2.3", "--stdio"}; !reflect.DeepEqual(locked.LauncherIdentityArgs, wantIdentity) {
		t.Fatalf("launcher identity args = %v, want %v", locked.LauncherIdentityArgs, wantIdentity)
	}
	if locked.LauncherDigest == "" {
		t.Fatal("launcher digest is empty")
	}
	if SchemaCacheKey(locked) != SchemaCacheKey(spec) {
		t.Fatal("host-local launcher lock changed the schema cache key")
	}
}

func TestStoredLauncherEnforcementFlagPreservesAuthorizedIdentity(t *testing.T) {
	cases := []struct {
		name, command, server, locator, resolved, enforcementFlag string
		args                                                      []string
	}{
		{
			name: "npx", command: "npx", server: "chrome-devtools",
			locator: "chrome-devtools-mcp@latest", resolved: "chrome-devtools-mcp@1.6.0",
			enforcementFlag: "--offline", args: []string{"-y", "chrome-devtools-mcp@latest", "--slim"},
		},
		{
			name: "bunx", command: "bunx", server: "browser",
			locator: "browser-mcp@latest", resolved: "browser-mcp@2.3.4",
			enforcementFlag: "--no-install", args: []string{"browser-mcp@latest", "--stdio"},
		},
		{
			name: "uvx", command: "uvx", server: "python-tools",
			locator: "python-tools", resolved: "python-tools==3.2.1",
			enforcementFlag: "--offline", args: []string{"python-tools", "--stdio"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			command := filepath.Join(dir, tc.command)
			if runtime.GOOS == "windows" {
				command += ".exe"
			}
			if err := os.WriteFile(command, []byte("test launcher"), 0o755); err != nil {
				t.Fatal(err)
			}
			manager := mcplaunch.NewManager(filepath.Join(t.TempDir(), mcplaunch.StateFilename), dir)
			lock := mcplaunch.LauncherLock{
				Server: tc.server, Locator: digestText(tc.locator),
				ResolvedVersion: tc.resolved, ContentSHA256: digestText("integrity"),
				Workspace: manager.WorkspaceFingerprint(),
			}
			spec := Spec{
				Name: tc.server, Command: command, Args: tc.args,
				LaunchManager: manager, ConfigSource: "project_config", RequireLaunchApproval: true,
			}
			locator, mutable := mutableLauncherLocator(spec)
			if !mutable {
				t.Fatalf("%s launcher was not recognized as mutable", tc.command)
			}
			preflight := spec
			applyLauncherResolution(&preflight, locator, lock, false)
			approvedIdentity, err := projectLaunchIdentityDigest(context.Background(), preflight)
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Authorize(spec.Name, spec.ConfigSource, approvedIdentity); err != nil {
				t.Fatal(err)
			}
			if err := manager.PutLauncherLock(lock); err != nil {
				t.Fatal(err)
			}
			locked, err := applyStoredLauncherLock(spec)
			if err != nil {
				t.Fatal(err)
			}
			if !stringSliceContains(locked.LaunchArgs, tc.enforcementFlag) || stringSliceContains(locked.LauncherIdentityArgs, tc.enforcementFlag) {
				t.Fatalf("launch args = %v, identity args = %v, enforcement flag = %q", locked.LaunchArgs, locked.LauncherIdentityArgs, tc.enforcementFlag)
			}
			restartIdentity, err := projectLaunchIdentityDigest(context.Background(), locked)
			if err != nil {
				t.Fatal(err)
			}
			if restartIdentity != approvedIdentity {
				t.Fatalf("stored-lock identity changed after enforcement: approved=%s restart=%s", approvedIdentity, restartIdentity)
			}
			if authorized, changed, err := manager.LaunchAuthorized(spec.Name, spec.ConfigSource, restartIdentity); err != nil || !authorized || changed {
				t.Fatalf("stored-lock launch authorization = (authorized=%v, changed=%v, err=%v)", authorized, changed, err)
			}
		})
	}
}

func TestStoredUVXFromLauncherLockKeepsFromValueAdjacent(t *testing.T) {
	manager := mcplaunch.NewManager(filepath.Join(t.TempDir(), mcplaunch.StateFilename), "/workspace")
	lock := mcplaunch.LauncherLock{
		Server: "python-tools", Locator: digestText("python-tools"),
		ResolvedVersion: "python-tools==3.2.1", ContentSHA256: digestText("integrity"),
	}
	if err := manager.PutLauncherLock(lock); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Name: "python-tools", Command: "uvx",
		Args:          []string{"--from", "python-tools", "python-tools-server", "--stdio"},
		LaunchManager: manager,
	}
	locked, err := applyStoredLauncherLock(spec)
	if err != nil {
		t.Fatal(err)
	}
	wantLaunch := []string{"--offline", "--from", "python-tools==3.2.1", "python-tools-server", "--stdio"}
	if !reflect.DeepEqual(locked.LaunchArgs, wantLaunch) {
		t.Fatalf("launch args = %v, want %v", locked.LaunchArgs, wantLaunch)
	}
	wantIdentity := []string{"--from", "python-tools==3.2.1", "python-tools-server", "--stdio"}
	if !reflect.DeepEqual(locked.LauncherIdentityArgs, wantIdentity) {
		t.Fatalf("launcher identity args = %v, want %v", locked.LauncherIdentityArgs, wantIdentity)
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestMutableLauncherRejectsAmbiguousFlagValue(t *testing.T) {
	locator, mutable := mutableLauncherLocator(Spec{Command: "npx", Args: []string{"--node-options", "--inspect", "server"}})
	if !mutable || locator.value != "" {
		t.Fatalf("ambiguous locator = %+v, mutable=%v", locator, mutable)
	}
}

func TestResolvePyPIPackagePinsVersionAndFileDigests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/demo/json" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"info":{"version":"2.4.1"},"urls":[{"digests":{"sha256":"bbb"}},{"digests":{"sha256":"aaa"}}]}`))
	}))
	defer server.Close()
	oldBase := pypiBaseURL
	pypiBaseURL = server.URL
	defer func() { pypiBaseURL = oldBase }()
	resolved, digest, err := resolvePyPIPackage(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "demo==2.4.1" || digest != digestText("aaa\nbbb") {
		t.Fatalf("resolution = %q %q", resolved, digest)
	}
}

func TestResolveExactGitLocatorDoesNotNeedNetwork(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	locator := "git+https://example.invalid/server.git@" + commit
	resolved, digest, err := resolveGitLocator(context.Background(), Spec{}, locator)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != commit || digest != digestText(commit) {
		t.Fatalf("resolution = %q %q", resolved, digest)
	}
}

func TestGitLauncherLockDoesNotPersistCredentialedLocator(t *testing.T) {
	home := t.TempDir()
	manager := mcplaunch.NewManager(filepath.Join(home, mcplaunch.StateFilename), "/workspace")
	locator := "git+https://user:secret-token@example.test/server.git@main"
	commit := "0123456789abcdef0123456789abcdef01234567"
	lock := mcplaunch.LauncherLock{
		Server: "git-server", Locator: digestText(locator), ResolvedVersion: commit, ContentSHA256: digestText(commit),
	}
	if err := manager.PutLauncherLock(lock); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(manager.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret-token") || strings.Contains(string(body), "user:") {
		t.Fatal("launcher security state persisted URL credentials")
	}
	spec := Spec{Name: "git-server", Command: "npx", Args: []string{locator}, LaunchManager: manager}
	got, err := applyStoredLauncherLock(spec)
	if err != nil {
		t.Fatal(err)
	}
	want := "git+https://user:secret-token@example.test/server.git@" + commit
	if len(got.LaunchArgs) != 2 || got.LaunchArgs[1] != want || got.LaunchArgs[0] != "--offline" {
		t.Fatalf("reconstructed git launch args = %v, want offline + exact original locator", got.LaunchArgs)
	}
}

func TestNPMPackageName(t *testing.T) {
	cases := map[string]string{
		"server":              "server",
		"server@^1":           "server",
		"@scope/server":       "@scope/server",
		"@scope/server@1.2.3": "@scope/server",
		"file:../server":      "",
		"github/acme":         "",
	}
	for input, want := range cases {
		if got := npmPackageName(input); got != want {
			t.Errorf("npmPackageName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFullGitCommitAcceptsOnlyCompleteObjectNames(t *testing.T) {
	sha1Commit := strings.Repeat("0123456789", 4)         // 40 hex
	sha256Commit := strings.Repeat("0123456789abcdef", 4) // 64 hex
	for value, want := range map[string]bool{
		sha1Commit:         true,
		sha256Commit:       true,
		sha1Commit[:39]:    false, // abbreviation
		sha1Commit + "a":   false, // 41-hex custom ref: resolve via ls-remote
		sha256Commit[:63]:  false,
		sha256Commit + "a": false,
		"main":             false,
		"":                 false,
	} {
		if got := fullGitCommit.MatchString(value); got != want {
			t.Errorf("fullGitCommit(%d hex %q...) = %v, want %v", len(value), value[:min(8, len(value))], got, want)
		}
	}

}

func TestResolvePyPIPackageRejectsWildcardBeforeNetwork(t *testing.T) {
	if _, _, err := resolvePyPIPackage(context.Background(), "server==2.4.*"); err == nil || !strings.Contains(err.Error(), "wildcard") {
		t.Fatalf("wildcard uvx locator resolved: %v", err)
	}
}
