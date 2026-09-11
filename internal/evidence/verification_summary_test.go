package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestVerificationCommandSummaryRecommendationsAreRecognized guards the
// single structured source used to render the summary and exercise the
// classifier. Every concrete example must render and remain accepted.
func TestVerificationCommandSummaryRecommendationsAreRecognized(t *testing.T) {
	summary := VerificationCommandSummary()
	for _, recommendation := range verificationCommandRecommendations() {
		for _, command := range recommendation.examples {
			if !strings.Contains(summary, command) {
				t.Errorf("summary family %q missing concrete example %q: %s", recommendation.label, command, summary)
			}
			if !IsVerificationCommand(command) {
				t.Errorf("summary family %q advertises %q, but classifier rejects it", recommendation.label, command)
			}
		}
	}
}

func TestGoBuildAlwaysCountsAsMutation(t *testing.T) {
	// Even ./... can expand to one main package and write a root executable.
	// The static classifier cannot know package expansion or inherited GOFLAGS,
	// so every go build form must fail closed as a mutation.
	for _, command := range []string{
		"go build",
		"go build .",
		"go build ./cmd/reasonix",
		"go build -race ./cmd/reasonix",
		"go build ./...",
		"go build -tags integration ./...",
		"go build -tags=integration ./...",
		"go build -race -trimpath ./...",
		"go build -p 2 -mod=readonly ./...",
		"go build -buildvcs ./...",
		"go build -buildvcs=auto -- ./...",
		"go build -tags ./... ./cmd/reasonix",
		"go build -coverpkg ./... ./cmd/reasonix",
		"go build -pkgdir ./... ./cmd/reasonix",
		"go build -pkgdir=./... ./cmd/reasonix",
		"go build -o reasonix ./...",
		"go build -o=reasonix ./...",
		"go build -mod=mod ./...",
		"go build -n ./...",
		"go build -work ./...",
		"go build -future-flag ./...",
		"go build ./... -o reasonix",
	} {
		if IsVerificationCommand(command) {
			t.Errorf("%q must not count as non-mutating verification", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if !ToolCallMutates("bash", args, false) {
			t.Errorf("%q must be classified as a mutation", command)
		}
	}
	if strings.Contains(VerificationCommandSummary(), "go build") {
		t.Fatal("recovery summary must not recommend go build as a first-line verifier")
	}
}

func TestTSCVerificationRequiresExplicitNoEmit(t *testing.T) {
	for _, command := range []string{
		"tsc --noEmit",
		"tsc --noEmit=true",
		"tsc --project tsconfig.json --noEmit",
		"tsc --noEmit --listFiles",
		"tsc --noEmit --pretty false",
	} {
		if !IsVerificationCommand(command) {
			t.Errorf("%q should be recognized as an explicit no-emit type check", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if ToolCallMutates("bash", args, false) {
			t.Errorf("%q should remain a non-mutating verification", command)
		}
	}

	for _, command := range []string{
		"tsc",
		"tsc --outDir dist",
		"tsc --noEmit=false",
		"tsc --noEmit false",
		"tsc --noEmit=true --noEmit=false",
		"tsc --noEmit --incremental --tsBuildInfoFile victim.ts",
		"tsc --noEmit --incremental --tsBuildInfoFile=victim.ts",
		"tsc --noEmit --generateTrace trace-dir",
		"tsc --noEmit --generateTrace=trace-dir",
		"tsc --noEmit --generateCpuProfile profile.cpuprofile",
		"tsc --noEmit --generateCpuProfile=profile.cpuprofile",
		"tsc --noEmit --init",
		"tsc --noEmit --help",
		"tsc --noEmit -h",
		"tsc --noEmit -?",
		"tsc --noEmit --all",
		"tsc --noEmit --version",
		"tsc --noEmit -v",
		"tsc --noEmit --showConfig",
		"tsc --noEmit --listFilesOnly",
		"tsc --noEmit --noCheck",
		"tsc --noEmit --watch",
		"tsc --noEmit -w",
		"tsc --build --noEmit",
		"tsc -b --noEmit",
		"tsc --build --clean --noEmit",
	} {
		if IsVerificationCommand(command) {
			t.Errorf("%q is not a bounded no-emit type check and must not count as verification", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if !ToolCallMutates("bash", args, false) {
			t.Errorf("%q must fail closed as a mutation", command)
		}
	}

	if !strings.Contains(VerificationCommandSummary(), "tsc --noEmit") {
		t.Fatal("recovery summary must render the concrete no-emit TypeScript command")
	}
}

func TestSwiftTestRecognizedAsVerification(t *testing.T) {
	// swift test runs the SwiftPM test suite; the build cache lands in the
	// package's own .build directory, mirroring cargo test acceptance. Other
	// swift subcommands emit binaries, run arbitrary code, or mutate the
	// package graph and must fail closed as mutations.
	for _, command := range []string{
		"swift test",
		"swift test --parallel",
		"swift test --filter SomeTests",
		"swift test --enable-code-coverage",
	} {
		if !IsVerificationCommand(command) {
			t.Errorf("%q should be recognized as a read-only Swift test verifier", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if ToolCallMutates("bash", args, false) {
			t.Errorf("%q should remain a non-mutating verification", command)
		}
	}

	for _, command := range []string{
		"swift",
		"swift build",
		"swift build -c release",
		"swift run",
		"swift package resolve",
		"swift package update",
		"swift test --xunit-output report.xml",
		"swift test --xunit-output=report.xml",
		"swift test --scratch-path /tmp/out",
		"swift test --build-path /tmp/out",
		"swift test --build-path=/tmp/out",
		"swift test --event-stream-output-path /tmp/events.json",
		"swift test --experimental-event-stream-output /tmp/events.json",
		"swift test --attachments-path /tmp/attachments",
		"swift test --experimental-attachments-path /tmp/attachments",
		"swift test --cache-path /tmp/cache",
		"swift test --help",
		"swift test --list-tests",
	} {
		if IsVerificationCommand(command) {
			t.Errorf("%q can build, run, or mutate the package and must not count as verification", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if !ToolCallMutates("bash", args, false) {
			t.Errorf("%q must fail closed as a mutation", command)
		}
	}

	if !strings.Contains(VerificationCommandSummary(), "swift test") {
		t.Fatal("recovery summary must render the concrete Swift test command")
	}
}

func TestPythonCompileallAlwaysCountsAsMutation(t *testing.T) {
	for _, command := range []string{
		"python -m compileall .",
		"python3 -m compileall -q src/",
		"python -m compileall -b package.py",
	} {
		if IsVerificationCommand(command) {
			t.Errorf("%q writes bytecode and must not count as verification", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if !ToolCallMutates("bash", args, false) {
			t.Errorf("%q writes bytecode and must be classified as a mutation", command)
		}
	}
	if strings.Contains(VerificationCommandSummary(), "compileall") {
		t.Fatal("recovery summary must not recommend bytecode-emitting compileall")
	}
}

func TestNpxVerificationUsesSafeKnownRunners(t *testing.T) {
	for _, command := range []string{
		"npx vitest run src/lib/foo.test.ts",
		"npx vitest@1.6.0 run src/lib/foo.test.ts",
		"npx jest src/lib/foo.test.ts",
		"npx mocha test/",
		"npx ava",
		"npx eslint src/",
		"npx prettier --check .",
		"npx prettier --list-different src/",
		"npx tsc --noEmit",
		"npx tsc --project tsconfig.json --noEmit",
		"npx mocha test/ 2>&1 | tail -40",
	} {
		if !IsVerificationCommand(command) {
			t.Errorf("%q should be recognized as a known read-only npx verifier", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if ToolCallMutates("bash", args, false) {
			t.Errorf("%q should remain non-mutating verification", command)
		}
	}

	for _, command := range []string{
		"npx --yes vitest run",
		"npx eslint@npm:evil src/",
		"npx vitest@file:../fake run",
		"npx ./eslint src/",
		"npx /tmp/eslint src/",
		"npx playwright test",
		"npx cypress run",
		"npx tsx check.ts",
		"npx ts-node check.ts",
		"npx eslint src/ --fix",
		"npx eslint src/ --output-file report.txt",
		"npx prettier --write .",
		"npx prettier .",
		"npx tsc",
		"npx tsc --noEmit --tsBuildInfoFile victim.ts",
		"npx tsc --noEmit --generateTrace trace-dir",
		"npx tsc --noEmit --init",
		"npx tsc --noEmit --showConfig",
		"npx tsc --noEmit --listFilesOnly",
		"npx tsc --noEmit --noCheck",
		"npx tsc --noEmit --watch",
		"npx tsc --build --noEmit",
	} {
		if IsVerificationCommand(command) {
			t.Errorf("%q can install, execute, or write output and must fail closed", command)
		}
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		if !ToolCallMutates("bash", args, false) {
			t.Errorf("%q must remain a mutation", command)
		}
	}

	if strings.Contains(VerificationCommandSummary(), "npx") {
		t.Fatal("self-installing npx commands must not be first-line recovery recommendations")
	}
}

// TestVerificationCommandSummaryNamesTheDeadlockTraps ensures the summary
// explicitly warns about the two command shapes that used to send models into
// the readiness deadlock loop: inline interpreters (blocked before execution)
// and read-only inspection commands (executed but never classified as
// verification).
func TestVerificationCommandSummaryNamesTheDeadlockTraps(t *testing.T) {
	s := VerificationCommandSummary()
	for _, want := range []string{
		"python -c",
		"node -e",
		"grep/find/cat/wc",
		"NOT verification",
		"blocked in delivery mode",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("summary should warn about trap %q, got: %s", want, s)
		}
	}
}

// TestVerificationCommandSummaryAcceptedPipeline ensures the escape hatch
// advertised by the summary (a read-only extraction pipeline ending in a
// recognized verifier) really is accepted by the classifier.
func TestVerificationCommandSummaryAcceptedPipeline(t *testing.T) {
	for _, cmd := range []string{
		"tail -n +1 out.json | node --check -",
		"cat file.js | node --check -",
	} {
		if !IsVerificationCommand(cmd) {
			t.Errorf("advertised read-only pipeline %q rejected by classifier", cmd)
		}
	}
}
