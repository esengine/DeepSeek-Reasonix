package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/safety/shellsafe"
	"reasonix/internal/state/sessiontemp"
)

const CodeDestructiveTarget = "shell.destructive_target"

const CodeShellAnalysisUnknown = "shell.analysis_unknown"
const CodeDeleteSequence = "shell.delete_sequence"
const CodeDeleteNonliteral = "shell.delete_nonliteral"
const CodeDeleteOption = "shell.delete_option"
const CodeShellSyntax = "shell.syntax_error"
const CodeParserUnavailable = "shell.parser_unavailable"
const CodeParserTimeout = "shell.parser_timeout"

func deleteRefusal(code, message string) error {
	return tool.Refusal{Code: code, Message: "Shell command refused before execution: " + message}
}

func (b bash) prepareGuardedLaunch(ctx context.Context, sh sandbox.Shell, p bashParams, args json.RawMessage) (sandbox.Prepared, pipeStatusProbe, *sessiontemp.Lease, error) {
	if err := b.refuseCommand(ctx, sh, p.Command); err != nil {
		return sandbox.Prepared{}, pipeStatusProbe{}, nil, err
	}
	return b.prepareLaunch(ctx, sh, p, args)
}

func (b bash) refuseCommand(ctx context.Context, sh sandbox.Shell, command string) error {
	if err := b.refuseExternalRef(command); err != nil {
		return err
	}
	if err := checkCommandLine(unconfinedShellArgv(sh, command)); err != nil {
		return err
	}
	return b.refuseDestructiveDelete(ctx, sh, command)
}

func (b bash) refuseDestructiveDelete(ctx context.Context, sh sandbox.Shell, command string) error {
	b.sessionTemp = b.sessionTempManager(ctx)
	seen := deleteJudgement{}
	err := b.judgeDelete(ctx, sh, command, 0, false, &seen)
	if err == nil && seen.containsDelete && seen.dynamicPayload {
		return deleteRefusal(CodeShellAnalysisUnknown, "Use a literal interpreter payload or split it from the recursive delete.")
	}
	return err
}

type deleteJudgement struct{ containsDelete, dynamicPayload bool }

func (b bash) judgeDelete(ctx context.Context, sh sandbox.Shell, command string, depth int, priorUnsafe bool, seen *deleteJudgement) error {
	if depth > 12 {
		return deleteRefusal(CodeShellAnalysisUnknown, "Nested interpreter extent is unknown; use a direct delete with a literal workspace path.")
	}
	analysis, err := shellparse.AnalyzeDeleteCalls(command)
	powerShell := sh.Kind == sandbox.ShellPowerShell
	if powerShell {
		analysis, err = analyzePowerShellDelete(ctx, sh, command)
	}
	if err != nil {
		code := CodeShellSyntax
		message := "Fix the shell syntax and retry: " + err.Error()
		if powerShell {
			code = CodeParserUnavailable
			message = "The host shell parser is unavailable; retry when the host parser is restored."
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			code = CodeParserTimeout
			message = "The host shell parser timed out or was canceled; retry the call."
		}
		return deleteRefusal(code, message)
	}
	if analysis.SyntaxError != "" {
		return deleteRefusal(CodeShellSyntax, "Fix the shell syntax and retry: "+analysis.SyntaxError)
	}
	unsafe := priorUnsafe
	directories := []string{b.deleteCwd()}
	if len(b.sb.WriteRoots) == 0 {
		b.sb.WriteRoots = []string{b.deleteCwd()}
	}
	for _, call := range analysis.Calls {
		payload, inner, interpreter := deleteInterpreter(call, sh)
		if interpreter {
			for _, arg := range call.Args {
				if arg == "" {
					seen.dynamicPayload = true
				}
			}
			if payload == "" {
				seen.dynamicPayload = true
			} else {
				for _, dir := range directories {
					child := b
					child.workDir = dir
					if err := child.judgeDelete(ctx, inner, payload, depth+1, unsafe || call.Unsafe, seen); err != nil {
						return err
					}
				}
			}
			unsafe = true
			continue
		}
		if deleteDirectoryChange(call.Name) {
			next, known := inferDeleteDirectories(directories, call)
			directories = next
			unsafe = unsafe || !known
			continue
		}
		extent := shellsafe.AnalyzeRecursiveDelete(call.Name, call.Args, powerShell)
		if !extent.Recursive {
			unsafe = true
			continue
		}
		seen.containsDelete = true
		if call.Name == "" || extent.DynamicCommand {
			return deleteRefusal(CodeShellAnalysisUnknown, "A dynamic executable has recursive-delete arguments; use a literal executable and a literal workspace path.")
		}
		if unsafe || call.Unsafe {
			return deleteRefusal(CodeDeleteSequence, "Split the call; only literal directory changes may precede a recursive delete. Assignments and nested control flow cannot establish a safe target.")
		}
		if extent.UnknownOption {
			return deleteRefusal(CodeDeleteOption, "Use known delete options and common parameters with explicit values.")
		}
		for _, target := range extent.Targets {
			if !literalDeletePath(target) {
				return deleteRefusal(CodeDeleteNonliteral, "Use a literal target path strictly inside the workspace or granted write roots; expansions, variables, globs and pipeline input are not bounded.")
			}
			for _, dir := range directories {
				scoped := b
				scoped.workDir = dir
				if !scoped.boundedDeleteTarget(target) {
					return deleteRefusal(CodeDestructiveTarget, "Use a target strictly inside the workspace or granted write roots; home, filesystem roots, workspace ancestors and out-of-scope paths are protected in every approval mode.")
				}
			}
		}
	}
	return nil
}

func deleteDirectoryChange(name string) bool {
	switch shellsafe.ExecutableBase(name) {
	case "cd", "set-location", "sl", "pushd", "push-location":
		return true
	}
	return false
}

func inferDeleteDirectories(directories []string, call shellparse.DeleteCall) ([]string, bool) {
	if len(directories) >= 64 || call.Unsafe || len(call.Args) != 1 || !literalDeletePath(call.Args[0]) {
		return directories, false
	}
	previous := append([]string(nil), directories...)
	for _, dir := range previous {
		path := call.Args[0]
		if !filepath.IsAbs(path) {
			path = dir + string(filepath.Separator) + path
		}
		directories = append(directories, path)
	}
	return directories, true
}

func literalDeletePath(target string) bool {
	return target != "" && !strings.ContainsAny(target, "$%*?[]{}`\x00") && tildeIsLiteral(target)
}

var shortNameSegment = regexp.MustCompile(`^[^\\/~]+~[0-9]+$`)

// tildeIsLiteral accepts a tilde only inside a Windows 8.3 short-name segment
// of a drive-letter path; every other tilde can expand to a home directory.
func tildeIsLiteral(target string) bool {
	if !strings.Contains(target, "~") {
		return true
	}
	if len(target) < 3 || !isDriveLetter(target[0]) || target[1] != ':' || target[2] != '\\' && target[2] != '/' {
		return false
	}
	for _, segment := range strings.FieldsFunc(target[3:], func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.Contains(segment, "~") && !shortNameSegment.MatchString(segment) {
			return false
		}
	}
	return true
}

func isDriveLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (b bash) deleteCwd() string {
	if b.workDir != "" {
		return b.workDir
	}
	cwd, _ := os.Getwd()
	return cwd
}

func (b bash) boundedDeleteTarget(target string) bool {
	if !literalDeletePath(target) {
		return false
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(target, "/") || runtime.GOOS == "windows" && strings.HasPrefix(target, "\\") && !filepath.IsAbs(target) {
		return false
	}
	// Provider-qualified and drive-relative paths do not have ordinary filesystem scope.
	if strings.Contains(target, ":") && (!filepath.IsAbs(target) || strings.Contains(strings.TrimPrefix(target, filepath.VolumeName(target)), ":")) {
		return false
	}
	cwd := b.workDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if !filepath.IsAbs(target) {
		target = cwd + string(filepath.Separator) + target
	}
	resolved, err := realPath(target)
	if err != nil || filepath.Dir(resolved) == resolved {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	home, err = realPath(home)
	if err != nil || withinFold(resolved, home) {
		return false
	}
	workspacePath := b.workDir
	if len(b.sb.WriteRoots) > 0 {
		workspacePath = b.sb.WriteRoots[0]
	}
	workspace, err := realPath(workspacePath)
	if err != nil || withinFold(resolved, workspace) {
		return false
	}
	roots := b.sb.WriteRoots
	if len(roots) == 0 {
		roots = []string{workspace}
	}
	if temp := b.sessionTemp.Dir(); temp != "" {
		roots = append(append([]string(nil), roots...), temp)
	}
	for _, root := range realRoots(roots) {
		if within(root, resolved) && !withinFold(resolved, root) {
			return true
		}
	}
	return false
}
