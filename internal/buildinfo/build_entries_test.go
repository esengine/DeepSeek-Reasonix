package buildinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sourceRevisionLinkerTarget = "reasonix/internal/buildinfo.SourceRevision"

func readBuildEntry(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func requireBuildEntryText(t *testing.T, path, source string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(source, want) {
			t.Errorf("%s missing %q", path, want)
		}
	}
}

func TestOfficialCLIBuildEntriesInjectSourceRevision(t *testing.T) {
	makefile := readBuildEntry(t, "Makefile")
	requireBuildEntryText(t, "Makefile", makefile,
		"CLI_LDFLAGS := $(LDFLAGS) -X "+sourceRevisionLinkerTarget+"=$(SOURCE_REVISION)",
		`go build -ldflags "$(CLI_LDFLAGS)" -o bin/reasonix$(GOEXE) ./cmd/reasonix`,
		`go build -ldflags "$(CLI_LDFLAGS)" -o dist/reasonix-$$os-$$arch$$ext ./cmd/reasonix`,
	)
	// The plugin example is not a CLI/Remote peer and should not gain an unused
	// buildinfo dependency merely because it shares the Makefile build target.
	requireBuildEntryText(t, "Makefile", makefile,
		`go build -ldflags "$(LDFLAGS)" -o bin/reasonix-plugin-example$(GOEXE)`,
	)

	goreleaser := readBuildEntry(t, ".goreleaser.yaml")
	requireBuildEntryText(t, ".goreleaser.yaml", goreleaser,
		"-X "+sourceRevisionLinkerTarget+"={{ .Env.GITHUB_SHA }}",
	)

	npmBuild := readBuildEntry(t, "npm/build.mjs")
	requireBuildEntryText(t, "npm/build.mjs", npmBuild,
		`const sourceRevision = resolveSourceRevision();`,
		"-X "+sourceRevisionLinkerTarget+"=${sourceRevision}",
		`"./cmd/reasonix"`,
	)
}

func TestDesktopBuildInjectsSourceRevisionOnlyIntoWailsBinary(t *testing.T) {
	script := readBuildEntry(t, "scripts/desktop-build.sh")
	requireBuildEntryText(t, "scripts/desktop-build.sh", script,
		`SOURCE_REVISION="$(resolve_source_revision)"`,
		"-X "+sourceRevisionLinkerTarget+`=$SOURCE_REVISION`,
		`build_args+=(-platform "$PLATFORM" -ldflags "$ldflags")`,
	)
	if got := strings.Count(script, sourceRevisionLinkerTarget); got != 1 {
		t.Fatalf("SourceRevision linker target occurs %d times, want only the Wails main binary injection", got)
	}
	resolveAt := strings.Index(script, `SOURCE_REVISION="$(resolve_source_revision)"`)
	mutateAt := strings.Index(script, `node -e 'const fs=require("fs"),f="wails.json"`)
	if resolveAt < 0 || mutateAt < 0 || resolveAt >= mutateAt {
		t.Fatal("Desktop must capture source revision before mutating tracked wails.json")
	}

	guardStart := strings.Index(script, "build_guard() {")
	guardEnd := strings.Index(script, "stamp_windows_executable() {")
	if guardStart < 0 || guardEnd <= guardStart {
		t.Fatal("cannot locate build_guard function")
	}
	if strings.Contains(script[guardStart:guardEnd], sourceRevisionLinkerTarget) {
		t.Fatal("Reasonix Guard must not gain an unused buildinfo injection")
	}
}

func TestBuildRevisionResolversPreferGitHubSHAWithoutDevFallback(t *testing.T) {
	tests := []struct {
		path  string
		wants []string
	}{
		{
			path: "Makefile",
			wants: []string{
				`[ -n "$(strip $(GITHUB_SHA))" ]`,
				`git rev-parse HEAD`,
				`git status --porcelain`,
				`revision="$$revision+dirty"`,
			},
		},
		{
			path: "npm/build.mjs",
			wants: []string{
				`process.env.GITHUB_SHA?.trim()`,
				`["rev-parse", "HEAD"]`,
				`["status", "--porcelain"]`,
				`${revision}${dirty ? "+dirty" : ""}`,
			},
		},
		{
			path: "scripts/desktop-build.sh",
			wants: []string{
				`${GITHUB_SHA:-}`,
				`git -C "$ROOT" rev-parse HEAD`,
				`git -C "$ROOT" status --porcelain`,
				`revision="${revision}+dirty"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			source := readBuildEntry(t, tt.path)
			requireBuildEntryText(t, tt.path, source, tt.wants...)
			for _, forbidden := range []string{
				"SourceRevision=dev",
				"SOURCE_REVISION=dev",
				`sourceRevision = "dev"`,
			} {
				if strings.Contains(source, forbidden) {
					t.Errorf("%s contains generic revision fallback %q", tt.path, forbidden)
				}
			}
		})
	}
}
