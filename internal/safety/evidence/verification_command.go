package evidence

// Whether a shell command counts as verification, read from its parsed shape
// rather than its wording: which runner it invokes, and whether a flag turns
// that runner into something that writes a report instead of checking.

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/shellsafe"
)

// BashToolCallMixesMutationAndVerification reports whether a bash call combines
// a host-recognized verifier with another segment the host cannot prove is
// read-only. Delivery mode blocks this shape before execution. Besides avoiding
// accidental workspace changes during a check, this keeps scratch-file setup
// (for example, writing /tmp/check.js before node --check) from becoming the
// latest opaque mutation and invalidating otherwise valid delivery evidence.
func BashToolCallMixesMutationAndVerification(args json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	command := stringField(fields, "command")
	return bashContainsVerificationSegment(command) && bashMayMutate(command)
}

// BashToolCallMixesMutationAndMaskableVerification is the ordinary-mode subset of
// BashToolCallMixesMutationAndVerification: the same mixed shape, but only when
// the shell's exit status can actually hide the earlier step's failure.
//
// Delivery mode blocks the broad shape because a mutation invalidates the
// verification *receipt* regardless of exit status. Ordinary mode has no receipt
// to protect — its only concern is a result that looks successful while an
// earlier step failed. `build && test` cannot produce that (bash short-circuits
// and reports the failing status), so blocking it would reject the single most
// common shell shape in real projects for no safety gain. `build; test` can,
// and stays blocked.
func BashToolCallMixesMutationAndMaskableVerification(args json.RawMessage) bool {
	if !BashToolCallMixesMutationAndVerification(args) {
		return false
	}
	command, ok := bashCommandFromArgs(args)
	if !ok {
		return false
	}
	canMask, analyzed := shellparse.CanMaskEarlierFailure(command)
	return analyzed && canMask
}

// BashToolCallMasksVerificationExit reports the common `check; echo $?` shape.
// The trailing reporter makes the shell call itself succeed even when the
// verifier failed, so a successful tool receipt cannot prove the check passed.
// It is separated from the broader mixed-command classifier so the agent can
// give a precise recovery instruction instead of inviting repeated rewrites.
func BashToolCallMasksVerificationExit(args json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	command := strings.TrimSpace(stringField(fields, "command"))
	if command == "" || !bashContainsVerificationSegment(command) {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return false
	}
	seenVerifier := false
	for _, segment := range segments {
		normalized, _ := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		argv, malformed := shellparse.StaticFields(normalized)
		if malformed == "" && bashSegmentIsVerification(argv) {
			seenVerifier = true
			continue
		}
		if !seenVerifier || !strings.Contains(segment, "$?") {
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(segment))
		if strings.HasPrefix(lower, "echo ") || strings.HasPrefix(lower, "printf ") {
			return true
		}
	}
	return false
}

// BashToolCallUsesNonTerminalInlineInterpreter reports whether an opaque
// inline interpreter (python -c, node -e, …) is not the last top-level segment
// *and* a later segment can overwrite its exit status. Ordinary mode blocks that
// shape deterministically without rewriting the command. An `&&` chain is left
// alone: bash short-circuits it, so the interpreter's failure is still the
// call's exit status and nothing is hidden.
func BashToolCallUsesNonTerminalInlineInterpreter(args json.RawMessage) bool {
	command, ok := bashCommandFromArgs(args)
	if !ok {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) < 2 {
		// Unknown / unparseable syntax: do not pretend full analysis.
		return false
	}
	if canMask, analyzed := shellparse.CanMaskEarlierFailure(command); !analyzed || !canMask {
		return false
	}
	for i, segment := range segments {
		if !bashSegmentUsesOpaqueInlineInterpreter(segment) {
			continue
		}
		if i < len(segments)-1 {
			return true
		}
	}
	return false
}

func bashCommandFromArgs(args json.RawMessage) (string, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return "", false
	}
	command := strings.TrimSpace(stringField(fields, "command"))
	return command, command != ""
}

// ShellContractPreflightMessage is the model-facing recovery text when a
// deterministic shell contract blocks a call before launch.
func ShellContractPreflightMessage(reason string) string {
	switch reason {
	case "mixed":
		return "blocked: this command runs a verification check after a state-changing segment, separated so the " +
			"check's exit status would hide a failure in that earlier segment. " +
			"Chain them with '&&' so a failed step stops the command and stays the result, " +
			"or run the modification and the verification as separate calls."
	case "mixed_delivery":
		return "blocked: this command mixes a verification check with a segment that may write state. " +
			"Delivery refuses the mixture whatever the exit status does, so chaining with '&&' is refused too. " +
			"Run the state-changing preparation as its own call while a todo is in_progress, then run a read-only " +
			"verification command. For generated input, prefer a host-recognized read-only pipeline into the " +
			"verifier (for example: tail ... | head ... | node --check -) instead of writing a temporary file."
	case "mask_exit":
		return "blocked: the trailing echo/printf of $? masks the verifier's exit status, so this command would look successful even when the check failed. " +
			"Run the verifier by itself and let its exit status be the tool result."
	case "inline_nonterminal":
		return "blocked: an inline interpreter (python -c, node -e, …) is followed by a segment that can hide its failure. " +
			"Chain with '&&' so the interpreter's exit status survives, run it as the final command, " +
			"or use edit_file for file changes and put script source in a file."
	default:
		return "blocked: this shell command violates the host execution contract. " +
			"Use edit_file for modifications and a separate shell call for verification."
	}
}

func bashContainsVerificationSegment(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	segments, ok := verifierSearchSegments(command)
	if !ok {
		return false
	}
	for _, segment := range segments {
		normalized, _ := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		fields, malformed := shellparse.StaticFields(normalized)
		if malformed == "" && bashSegmentIsVerification(fields) {
			return true
		}
	}
	return false
}

func bashCommandIsVerification(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) == 0 {
		return false
	}
	found := false
	for _, segment := range segments {
		normalized, safeRedirects := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		if !safeRedirects {
			return false
		}
		fields, malformed := shellparse.StaticFields(normalized)
		if malformed != "" || len(fields) == 0 {
			return false
		}
		if bashSegmentIsVerification(fields) {
			found = true
			continue
		}
		if _, _, readOnly := shellsafe.CommandIsReadOnly(normalized); !readOnly {
			return false
		}
	}
	return found
}

// IsDeliveryVerificationCommand reports whether command is a host-recognized
// verification command for delivery finalization. Keep complete_step and the
// final-readiness gate on this single classifier so a sign-off cannot claim a
// command that the final gate will immediately reject.
func IsDeliveryVerificationCommand(command string) bool {
	return bashCommandIsVerification(command)
}

// verificationCommandRecommendations is the single source for the concrete
// model-readable examples and the family labels used to diagnose test failures.
// It is intentionally a safe recommended subset rather than an exhaustive
// rendering of bashSegmentIsVerification: accepted commands that may install
// dependencies or create workspace outputs should not be suggested as the
// first recovery action.
func verificationCommandRecommendations() []verificationCommandRecommendation {
	return []verificationCommandRecommendation{
		{label: "go test|vet", examples: []string{"go test ./...", "go vet ./..."}},
		{label: "git diff --check", examples: []string{"git diff --check"}},
		{label: "pytest/py.test", examples: []string{"pytest tests/", "py.test tests/"}},
		{label: "gotestsum", examples: []string{"gotestsum"}},
		{label: "staticcheck", examples: []string{"staticcheck ./..."}},
		{label: "golangci-lint", examples: []string{"golangci-lint run"}},
		{label: "tsc", examples: []string{"tsc --noEmit"}},
		{label: "mypy (no report flag)", examples: []string{"mypy src/"}},
		{label: "npm|pnpm|yarn|bun test|check|lint", examples: []string{"npm test", "pnpm check", "yarn lint", "bun test"}},
		{label: "npm run test|check|lint|typecheck", examples: []string{"npm run typecheck"}},
		{label: "cargo test|check|clippy", examples: []string{"cargo test", "cargo check", "cargo clippy"}},
		{label: "node --check|--test", examples: []string{"node --check index.js", "node --test"}},
		{label: "make|just test|check|lint|verify|ci", examples: []string{"make test", "just verify"}},
		{label: "python -m pytest|unittest", examples: []string{"python -m pytest", "python -m unittest"}},
		{label: "dotnet test", examples: []string{"dotnet test"}},
		{label: "swift test", examples: []string{"swift test"}},
		{label: "mvn|gradle test|check|verify", examples: []string{"mvn test", "gradle check"}},
	}
}

// VerificationCommandSummary returns compact, model-readable recovery
// guidance. It lists only recommended command families that the classifier
// accepts, while omitting known self-installing and direct workspace-output
// command forms from first-line guidance.
func VerificationCommandSummary() string {
	recommendations := verificationCommandRecommendations()
	commands := make([]string, 0, len(recommendations))
	for _, recommendation := range recommendations {
		commands = append(commands, recommendation.examples...)
	}
	return "recommended recognized verification commands: " + strings.Join(commands, ", ") + ". " +
		"Read-only inspection commands (grep/find/cat/wc/head/tail) are NOT verification; " +
		"inline interpreters (node -e, python -c) are blocked in delivery mode. " +
		"A read-only extraction pipeline ending in a recognized verifier " +
		"(e.g. tail -n +1 file | node --check -) is accepted."
}

func bashSegmentIsVerification(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(fields[0]))
	args := fields[1:]
	if hasCommandArg(args, "--fix", "--write", "-w", "--update", "-u") {
		return false
	}
	if hasWriteOutputFlag(args) {
		return false
	}
	switch base {
	case "go":
		if len(args) == 0 {
			return false
		}
		if args[0] == "vet" {
			return true
		}
		if args[0] == "test" {
			return !slices.ContainsFunc(args[1:], goTestFlagWritesFile)
		}
		// A package pattern can expand to one main package, so even `go build
		// ./...` may write a workspace binary. Package expansion and inherited
		// GOFLAGS are unavailable to this static classifier; fail closed for all
		// build forms and keep test/vet as the recognized Go verifiers.
		return false
	case "git":
		return len(args) > 1 && args[0] == "diff" && hasCommandArg(args[1:], "--check")
	case "pytest", "py.test", "gotestsum", "staticcheck", "golangci-lint":
		return true
	case "tsc":
		return tscSegmentIsVerification(args)
	case "mypy":
		return !slices.ContainsFunc(args, mypyFlagWritesReport)
	case "npm", "pnpm", "yarn", "bun", "cargo":
		if len(args) > 0 && hasCommandArg(args[:1], "test", "check", "lint", "clippy") {
			return true
		}
		return len(args) > 1 && args[0] == "run" && hasCommandArg(args[1:2], "test", "check", "lint", "typecheck")
	case "npx":
		return npxSegmentIsVerification(args)
	case "node":
		return nodeSegmentIsVerification(args)
	case "make", "just":
		return len(args) > 0 && hasCommandArg(args[:1], "test", "check", "lint", "verify", "ci")
	case "python", "python3":
		return len(args) > 1 && args[0] == "-m" && hasCommandArg(args[1:2], "pytest", "unittest")
	case "dotnet":
		return len(args) > 0 && args[0] == "test"
	case "swift":
		// swift test runs the SwiftPM test suite; build artifacts stay under
		// the package's own .build directory (including --enable-code-coverage
		// reports). Other swift subcommands (build/run/package) can write
		// binaries or mutate the package, so only the test form is a
		// recognized verifier. Explicit report destinations, attachment dirs,
		// and scratch-dir redirects are rejected by writeOutputFlags. Note
		// that swift test may run Package.swift build plugins (arbitrary
		// code) — the same trust boundary as go test / cargo test.
		if len(args) == 0 || args[0] != "test" {
			return false
		}
		// Control modes that do not run the test suite (help, listing) must
		// not count as verification; mirror the tsc treatment of --help.
		for _, arg := range args[1:] {
			name := strings.TrimLeft(strings.ToLower(arg), "-")
			if i := strings.IndexByte(name, '='); i >= 0 {
				name = name[:i]
			}
			switch name {
			case "help", "h", "version", "list-tests", "l":
				return false
			}
		}
		return true
	case "mvn", "mvnw", "gradle", "gradlew":
		return len(args) > 0 && hasCommandArg(args, "test", "check", "verify")
	}
	return false
}

// tscSegmentIsVerification accepts only one-shot, explicit no-emit type checks.
// Bare tsc commands may emit JavaScript, declarations, and source maps; control
// modes may write config, skip checking, exit after printing metadata, or watch
// indefinitely. Any explicit false value wins conservatively even if another
// no-emit flag appears in the same command.
func tscSegmentIsVerification(args []string) bool {
	noEmit := false
	for i, arg := range args {
		if tscFlagDisqualifiesVerification(arg) {
			return false
		}
		switch strings.ToLower(arg) {
		case "--noemit":
			if i+1 < len(args) && strings.EqualFold(args[i+1], "false") {
				return false
			}
			noEmit = true
		case "--noemit=true":
			noEmit = true
		case "--noemit=false":
			return false
		}
	}
	return noEmit
}

// tscFlagDisqualifiesVerification rejects modes that do not perform a bounded
// type check and destinations that write independently of JavaScript/declaration
// emit. Default incremental metadata remains conventional verifier cache;
// explicit output destinations and control modes fail closed as mutations.
func tscFlagDisqualifiesVerification(arg string) bool {
	name := strings.ToLower(arg)
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "--tsbuildinfofile", "--generatetrace", "--generatecpuprofile",
		"--init", "--help", "-h", "-?", "--all", "--version", "-v",
		"--showconfig", "--listfilesonly", "--nocheck", "--watch", "-w",
		"--build", "-b", "--clean":
		return true
	default:
		return false
	}
}

// npxSegmentIsVerification unwraps only known test runners invoked directly,
// with no npx control flags. Treating arbitrary npx packages as verification
// would let package installation or an opaque executable masquerade as a
// read-only check. Runner flags that update snapshots, write reports, or enable
// coverage are rejected by the caller and the checks below.
func npxSegmentIsVerification(args []string) bool {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return false
	}
	runner, ok := npxRunnerName(args[0])
	if !ok {
		return false
	}
	runnerArgs := args[1:]
	switch runner {
	case "vitest", "jest", "mocha", "ava", "eslint":
		// Known test/lint runners are verification unless an argument asks them
		// to update snapshots, collect coverage, or write a report.
	case "prettier":
		// Prettier without an explicit check mode formats to stdout and is not a
		// project verification receipt. Keep only its read-only check forms.
		if !hasCommandArg(runnerArgs, "--check", "-c", "--list-different") {
			return false
		}
	case "tsc":
		return tscSegmentIsVerification(runnerArgs)
	default:
		// Playwright/Cypress produce project reports, screenshots, or videos by
		// default; tsx/ts-node execute source. They remain mutations.
		return false
	}
	for _, arg := range runnerArgs {
		name := strings.ToLower(arg)
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		switch name {
		case "--update", "-u", "--updatesnapshot", "--update-snapshots",
			"--output-file", "-o", "--cache-location":
			return false
		}
		if name == "--coverage" || strings.HasPrefix(name, "--coverage.") {
			return false
		}
	}
	return true
}

// npxRunnerName accepts only a bare package name with an optional ordinary
// version or dist-tag suffix. Paths and package protocols such as
// eslint@npm:other-package must not inherit a known runner's trust boundary.
func npxRunnerName(spec string) (string, bool) {
	if spec == "" || strings.ContainsAny(spec, `/\`) {
		return "", false
	}
	name := strings.ToLower(spec)
	if strings.HasPrefix(name, "@") {
		return "", false
	}
	if i := strings.LastIndexByte(name, '@'); i >= 0 {
		if i == 0 || !plainNpxVersion(name[i+1:]) {
			return "", false
		}
		name = name[:i]
	}
	return name, true
}

func plainNpxVersion(version string) bool {
	if version == "" {
		return false
	}
	for _, r := range version {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '.', '-', '+', '_', '~', '^', '*':
			continue
		default:
			return false
		}
	}
	return true
}

func nodeSegmentIsVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Node CLI flags are case-sensitive: -c/--check is the syntax-only mode,
	// while -C/--conditions executes the target with custom export conditions.
	switch args[0] {
	case "--check", "-c":
		// Syntax-check mode does not execute the target. Fail closed on any
		// additional option: preload/eval/import flags could execute code before
		// the check and turn a purported verifier into an opaque mutation.
		for _, arg := range args[1:] {
			if arg != "-" && strings.HasPrefix(arg, "-") {
				return false
			}
		}
		return true
	case "--test":
		// Match the repository's treatment of other conventional test runners,
		// but fail closed on test-runner and Node runtime flags that write files.
		return !slices.ContainsFunc(args[1:], nodeTestFlagWritesFile)
	default:
		return false
	}
}

func nodeTestFlagWritesFile(arg string) bool {
	name := strings.ToLower(arg)
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "--cpu-prof", "--heap-prof", "--heapsnapshot-near-heap-limit", "--heapsnapshot-signal",
		"--localstorage-file", "--perf-basic-prof", "--perf-basic-prof-only-functions", "--perf-prof",
		"--prof", "--redirect-warnings", "--report-on-fatalerror", "--report-on-signal",
		"--report-uncaught-exception", "--test-reporter-destination", "--test-rerun-failures",
		"--test-update-snapshots", "--tls-keylog", "--trace-events-enabled":
		return true
	default:
		return false
	}
}

// mypyFlagWritesReport reports whether a mypy flag writes a report directory:
// every mypy report option follows the --<type>-report DIR shape (txt, html,
// xml, cobertura-xml, any-exprs, linecount, linecoverage, lineprecision), and
// mypy has no read-only flag with that suffix. --junit-xml is covered by the
// global write-output flags.
func mypyFlagWritesReport(arg string) bool {
	name := strings.ToLower(arg)
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	return strings.HasPrefix(name, "--") && strings.HasSuffix(name, "-report")
}

// goTestFlagWritesFile reports whether a go test flag writes a workspace
// artifact: -c/-o emit the test binary, -trace and the profile flags write
// profiles, and -artifacts/-testlogfile/-gocoverdir write test outputs. The
// short and ambiguous names stay out of writeOutputFlags because the
// dash-stripped global match would also hit node -c (a syntax-only check)
// and pytest --trace (a read-only debugger flag). go test flags accept
// single- and double-dash forms and an optional test. prefix that the go
// tool passes through to the test binary.
func goTestFlagWritesFile(arg string) bool {
	name := strings.ToLower(arg)
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	trimmed := strings.TrimLeft(name, "-")
	if len(trimmed) == len(name) || trimmed == "" {
		return false // not a flag
	}
	trimmed = strings.TrimPrefix(trimmed, "test.")
	switch trimmed {
	case "c", "o", "trace", "artifacts", "testlogfile", "gocoverdir",
		"coverprofile", "cpuprofile", "memprofile", "blockprofile", "mutexprofile":
		return true
	default:
		return false
	}
}

func completeStepVerificationCommands(args json.RawMessage) []string {
	var p struct {
		Evidence []struct {
			Kind    string `json:"kind"`
			Command string `json:"command"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil
	}
	var out []string
	for _, item := range p.Evidence {
		if item.Kind == "verification" && strings.TrimSpace(item.Command) != "" {
			out = append(out, strings.TrimSpace(item.Command))
		}
	}
	return out
}
