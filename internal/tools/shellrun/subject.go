package shellrun

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/base/shellparse"
)

// OperativeCommand returns command with its leading environment assignments
// dropped, and whether any were. The cut is positional, taken from the parsed
// command's own byte offsets, so quoting and substitutions survive as written.
// A command that does not parse, or has no such prefix, comes back untouched.
func OperativeCommand(command string) (string, bool) {
	file, err := shellparse.ParseBash(command)
	if err != nil || file == nil || len(file.Stmts) == 0 {
		return command, false
	}
	first := file.Stmts[0]
	// A pipeline or a list is its own node, and the assignments sit on the
	// command at its left edge.
	cmd := first.Cmd
	for {
		binary, ok := cmd.(*syntax.BinaryCmd)
		if !ok || binary.X == nil {
			break
		}
		cmd = binary.X.Cmd
	}
	call, ok := cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) == 0 || len(call.Args) == 0 {
		return command, false
	}
	// Only a prefix may be dropped. An assignment that is not where the command
	// starts belongs to something else in it.
	if call.Assigns[0].Pos().Offset() != first.Pos().Offset() {
		return command, false
	}
	at := int(call.Args[0].Pos().Offset())
	if at <= 0 || at >= len(command) {
		return command, false
	}
	return strings.TrimSpace(command[at:]), true
}
