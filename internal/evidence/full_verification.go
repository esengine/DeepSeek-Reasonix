package evidence

import (
	"path/filepath"
	"strings"

	"reasonix/internal/shellparse"
	"reasonix/internal/shellsafe"
)

// IsFullVerificationCommand reports whether a recognized verifier clearly
// covers the current project rather than one named package, file, or test.
// Project-declared checks remain authoritative at the task-contract layer;
// this conservative fallback is for repositories without declared checks.
func IsFullVerificationCommand(command string) bool {
	if !bashCommandIsVerification(command) {
		return false
	}
	if masks, ok := shellparse.CanMaskEarlierFailure(command); !ok || masks {
		return false
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return false
	}
	for _, segment := range segments {
		normalized, safe := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
		if !safe {
			continue
		}
		fields, ok := bashStaticArgv(normalized)
		if ok && fullVerificationArgv(fields) {
			return true
		}
	}
	return false
}

func fullVerificationArgv(fields []string) bool {
	if !bashSegmentIsVerification(fields) || len(fields) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(fields[0]))
	args := fields[1:]
	switch base {
	case "go":
		return fullGoVerification(args)
	case "pytest", "py.test":
		return onlyBroadVerificationTargets(args, ".", "./", "test", "test/", "tests", "tests/")
	case "gotestsum", "staticcheck":
		return len(args) == 0 || slicesContainFold(args, "./...")
	case "golangci-lint":
		return onlyBroadVerificationTargets(args)
	case "tsc":
		return onlyBroadVerificationTargets(args)
	case "mypy":
		return onlyBroadVerificationTargets(args, ".", "./", "src", "src/")
	case "npm", "pnpm", "yarn", "bun":
		return fullScriptVerification(args)
	case "cargo":
		return len(args) > 0 && onlyBroadVerificationTargets(args[1:])
	case "npx":
		return fullNpxVerification(args)
	case "node":
		return len(args) > 0 && args[0] == "--test" && onlyBroadVerificationTargets(args[1:])
	case "make", "just":
		return len(args) == 1
	case "python", "python3":
		return len(args) > 1 && args[0] == "-m" && onlyBroadVerificationTargets(args[2:])
	case "dotnet":
		return len(args) > 0 && args[0] == "test" && onlyBroadVerificationTargets(args[1:])
	case "swift":
		return len(args) > 0 && args[0] == "test" && onlyBroadVerificationTargets(args[1:])
	case "mvn", "mvnw", "gradle", "gradlew":
		return buildToolVerificationIsFull(args)
	default:
		return false
	}
}

func fullGoVerification(args []string) bool {
	if len(args) < 2 || (args[0] != "test" && args[0] != "vet") || !slicesContainFold(args[1:], "./...") {
		return false
	}
	if args[0] == "vet" {
		return true
	}
	for _, arg := range args[1:] {
		name := strings.TrimPrefix(strings.ToLower(arg), "-test.")
		name = strings.TrimLeft(name, "-")
		if before, _, ok := strings.Cut(name, "="); ok {
			name = before
		}
		switch name {
		case "run", "bench", "list", "skip":
			return false
		case "count":
			if strings.HasSuffix(strings.ToLower(arg), "=0") {
				return false
			}
		}
	}
	return true
}

func fullScriptVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	start := 1
	if args[0] == "run" {
		if len(args) < 2 {
			return false
		}
		start = 2
	}
	return onlyBroadVerificationTargets(args[start:])
}

func fullNpxVerification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	runner, ok := npxRunnerName(args[0])
	if !ok {
		return false
	}
	switch runner {
	case "tsc":
		return tscSegmentIsVerification(args[1:]) && onlyBroadVerificationTargets(args[1:])
	case "vitest", "jest", "mocha", "ava", "eslint", "prettier":
		return onlyBroadVerificationTargets(args[1:], ".", "./")
	default:
		return false
	}
}

func buildToolVerificationIsFull(args []string) bool {
	seenTask := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		switch strings.ToLower(arg) {
		case "test", "check", "verify":
			seenTask = true
		default:
			return false
		}
	}
	return seenTask
}

func onlyBroadVerificationTargets(args []string, broad ...string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !slicesContainFold(broad, arg) {
			return false
		}
	}
	return true
}

func slicesContainFold(items []string, want string) bool {
	for _, item := range items {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}
