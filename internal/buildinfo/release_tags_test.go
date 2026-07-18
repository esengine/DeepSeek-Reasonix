//go:build !windows

package buildinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCoordinatedReleaseTagGuard(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	repository := filepath.Join(t.TempDir(), "release-origin")
	runReleaseGit(t, "", "init", repository)
	runReleaseGit(t, repository, "config", "user.name", "Reasonix Test")
	runReleaseGit(t, repository, "config", "user.email", "reasonix-test@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "source.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runReleaseGit(t, repository, "add", "source.txt")
	runReleaseGit(t, repository, "commit", "-m", "first")
	first := strings.TrimSpace(runReleaseGit(t, repository, "rev-parse", "HEAD"))
	runReleaseGit(t, repository, "tag", "v9.8.7")
	// Include one annotated tag so the guard must compare its peeled commit,
	// not the distinct tag-object hash returned by ls-remote.
	runReleaseGit(t, repository, "tag", "-a", "npm-v9.8.7", "-m", "npm")
	runReleaseGit(t, repository, "tag", "desktop-v9.8.7")

	for _, tag := range []string{"v9.8.7", "npm-v9.8.7", "desktop-v9.8.7"} {
		t.Run("accept_"+tag, func(t *testing.T) {
			output, err := runReleaseTagGuard(t, repository, tag, first)
			if err != nil {
				t.Fatalf("guard failed: %v\n%s", err, output)
			}
			if !strings.Contains(output, "all resolve to "+first) {
				t.Fatalf("guard output = %q", output)
			}
		})
	}

	// workflow_dispatch starts on an arbitrary selected ref. The npm workflow
	// fetches the requested stable tag explicitly and peels it into a detached
	// commit before it builds, including when the release tag is annotated.
	manualCheckout := filepath.Join(t.TempDir(), "manual-stable-checkout")
	runReleaseGit(t, "", "init", manualCheckout)
	runReleaseGit(t, manualCheckout, "remote", "add", "origin", repository)
	runReleaseGit(t, manualCheckout, "fetch", "--no-tags", "origin", "refs/tags/npm-v9.8.7")
	runReleaseGit(t, manualCheckout, "checkout", "--detach", "FETCH_HEAD^{commit}")
	if got := strings.TrimSpace(runReleaseGit(t, manualCheckout, "rev-parse", "HEAD")); got != first {
		t.Fatalf("manual stable checkout = %s, want peeled commit %s", got, first)
	}

	if err := os.WriteFile(filepath.Join(repository, "source.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runReleaseGit(t, repository, "add", "source.txt")
	runReleaseGit(t, repository, "commit", "-m", "second")
	second := strings.TrimSpace(runReleaseGit(t, repository, "rev-parse", "HEAD"))
	runReleaseGit(t, repository, "tag", "-f", "desktop-v9.8.7", second)
	if output, err := runReleaseTagGuard(t, repository, "v9.8.7", first); err == nil ||
		!strings.Contains(output, "desktop-v9.8.7 resolves to "+second+", expected "+first) {
		t.Fatalf("mismatched tag guard = %v\n%s", err, output)
	}

	if output, err := runReleaseTagGuard(t, repository, "v1.2.3", first); err == nil ||
		!strings.Contains(output, "missing coordinated release tag v1.2.3") {
		t.Fatalf("missing tag guard = %v\n%s", err, output)
	}
}

func TestCoordinatedReleaseWorkflowContracts(t *testing.T) {
	workflows := map[string]string{
		".github/workflows/ci.yml":              readBuildEntry(t, ".github/workflows/ci.yml"),
		".github/workflows/release.yml":         readBuildEntry(t, ".github/workflows/release.yml"),
		".github/workflows/release-npm.yml":     readBuildEntry(t, ".github/workflows/release-npm.yml"),
		".github/workflows/release-desktop.yml": readBuildEntry(t, ".github/workflows/release-desktop.yml"),
	}
	for path, source := range workflows {
		t.Run("yaml_"+filepath.Base(path), func(t *testing.T) {
			var document any
			if err := yaml.Unmarshal([]byte(source), &document); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
		})
	}

	ci := workflows[".github/workflows/ci.yml"]
	requireBuildEntryText(t, ".github/workflows/ci.yml", ci,
		"go run ./cmd/remote-protocol-gen -check",
		"if: runner.os == 'Linux'",
	)

	release := workflows[".github/workflows/release.yml"]
	if got := strings.Count(release, "./scripts/verify-coordinated-release-tags.sh"); got != 2 {
		t.Fatalf("release.yml coordinated guards = %d, want cache and artifact jobs", got)
	}
	requireBuildEntryText(t, ".github/workflows/release.yml", release,
		`if [ "$GITHUB_SHA" != "$checkout_commit" ]`,
		`"$(git rev-parse HEAD)"`,
	)

	npm := workflows[".github/workflows/release-npm.yml"]
	if got := strings.Count(npm, "- name: Checkout requested stable tag"); got != 2 {
		t.Fatalf("release-npm.yml stable tag checkouts = %d, want cache and artifact jobs", got)
	}
	if got := strings.Count(npm, "./scripts/verify-coordinated-release-tags.sh"); got != 2 {
		t.Fatalf("release-npm.yml coordinated guards = %d, want cache and artifact jobs", got)
	}
	requireBuildEntryText(t, ".github/workflows/release-npm.yml", npm,
		`git check-ref-format "refs/tags/$tag"`,
		`git checkout --detach "FETCH_HEAD^{commit}"`,
		"unset GITHUB_SHA",
		"if: github.event_name != 'workflow_dispatch' || inputs.channel != 'canary'",
	)

	desktop := workflows[".github/workflows/release-desktop.yml"]
	stableRef := "ref: ${{ github.event_name == 'workflow_dispatch' && inputs.channel != 'canary' && inputs.tag || github.ref }}"
	if got := strings.Count(desktop, stableRef); got != 4 {
		t.Fatalf("release-desktop.yml source-pinned checkouts = %d, want all four jobs", got)
	}
	if got := strings.Count(desktop, "./scripts/verify-coordinated-release-tags.sh"); got != 2 {
		t.Fatalf("release-desktop.yml coordinated guards = %d, want cache and artifact jobs", got)
	}
	requireBuildEntryText(t, ".github/workflows/release-desktop.yml", desktop,
		"unset GITHUB_SHA",
		"if: github.event_name != 'workflow_dispatch' || inputs.channel != 'canary'",
		"desktop stable release requires a desktop-v* tag",
	)
}

func runReleaseGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	if directory != "" {
		command.Dir = directory
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func runReleaseTagGuard(t *testing.T, repository, tag, revision string) (string, error) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(root, "scripts", "verify-coordinated-release-tags.sh"), tag, revision)
	command.Dir = root
	command.Env = append(os.Environ(), "RELEASE_TAG_REMOTE="+repository)
	output, err := command.CombinedOutput()
	return string(output), err
}
