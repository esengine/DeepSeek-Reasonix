package hook

import (
	"context"
	"os/exec"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/base/shellparse"
)

// windowsPOSIXShellCommand runs a settings hook written in shell syntax through
// the bash the shell-form contract resolves. cmd.exe would fail on the first
// construct it cannot parse, and its complaint names the wrong cause.
func windowsPOSIXShellCommand(ctx context.Context, command string, options RuntimeOptions) (*exec.Cmd, bool, error) {
	if !requiresPOSIXShell(command) {
		return nil, false, nil
	}
	bash, err := resolveWindowsHookBash(options.BashPath)
	if err != nil {
		return nil, true, err
	}
	return exec.CommandContext(ctx, bash, "-c", command), true, nil
}

// requiresPOSIXShell reports whether a command's syntax carries meaning only a
// POSIX shell supplies: single-quote quoting, $ expansion, command
// substitution, here-documents. What cmd.exe does share — && chains, pipes,
// double quotes, plain redirects — is deliberately absent, so a batch-style
// hook keeps the interpreter it has always had.
func requiresPOSIXShell(command string) bool {
	file, err := shellparse.ParseBash(command)
	if err != nil || file == nil {
		return false
	}
	posix := false
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.SglQuoted, *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
			posix = true
		case *syntax.Redirect:
			posix = posix || isHereDocRedirect(n.Op)
		}
		return !posix
	})
	return posix
}

func isHereDocRedirect(op syntax.RedirOperator) bool {
	switch op {
	case syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		return true
	default:
		return false
	}
}
