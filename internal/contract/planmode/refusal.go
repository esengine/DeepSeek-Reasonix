package planmode

import (
	"fmt"
	"strings"

	"reasonix/internal/base/textutil"
)

// Why is what the caller's own classifier established about a call it could not
// admit. planmode carries it to the model; it never derives one.
type Why string

const (
	// WhyDeclaredWriter: the tool declares it can change state.
	WhyDeclaredWriter Why = "declared_writer"
	// WhyWriteArguments: the program may read, but these arguments make it write
	// or run another program.
	WhyWriteArguments Why = "write_arguments"
	// WhyUnknownProgram: the program is not in the host's read-only table.
	WhyUnknownProgram Why = "unknown_program"
	// WhyShellConstruct: the command uses syntax whose effect is only known when
	// it runs. Detail names the construct.
	WhyShellConstruct Why = "shell_construct"
	// WhyBackground: the command was started to outlive the call.
	WhyBackground Why = "background"
	// WhyNetworkPath: an operand may name a network path.
	WhyNetworkPath Why = "network_path"
	// WhyUndeclared: the tool never declared itself read-only and is not known to
	// write.
	WhyUndeclared Why = "undeclared"
)

// Proof is the classifier's answer for one call. Subject is the command or
// target the answer is about; Detail narrows Why (the program, the construct).
type Proof struct {
	Why     Why
	Subject string
	Detail  string
}

const planningExit = "While planning, these run normally: reading, searching and inspecting files; " +
	"read-only shell commands (one known reader such as ls, cat, grep or git log, with no variable expansion, " +
	"command substitution, assignment, redirection, background job, or arguments that write); " +
	"read-only delegation; todo_write; and ask. " +
	"To run this call as written, present the plan and stop: once the user approves, the call goes through the " +
	"ordinary Permissions and Sandbox path."

func shown(s string, lim textutil.PreviewLimit) string {
	out, _ := textutil.BoundLiteral(strings.TrimSpace(s), lim)
	return out
}

func causeLine(name string, p Proof) string {
	switch p.Why {
	case WhyDeclaredWriter:
		return fmt.Sprintf("%q is not a read-only tool, so it can change state outside this session.", name)
	case WhyWriteArguments:
		return fmt.Sprintf("the arguments given to %q make it write or run another program.", p.Detail)
	case WhyUnknownProgram:
		return fmt.Sprintf("%q is not in the host's read-only command table, so the host could not prove this command only reads.", p.Detail)
	case WhyShellConstruct:
		return fmt.Sprintf("the command uses %s, whose effect is only known when it runs, so the host could not prove it only reads.", p.Detail)
	case WhyBackground:
		return "the command was started in the background, which outlives the call."
	case WhyNetworkPath:
		return "an operand may name a network path, which the host cannot prove only reads."
	case WhyUndeclared:
		return fmt.Sprintf("%q does not declare itself read-only, so the host could not prove it only reads.", name)
	}
	return fmt.Sprintf("the host could not prove %q only reads.", name)
}

func refusalMessage(call Call) string {
	name := shown(call.Name, textutil.PreviewIdentity)
	call.Proof.Detail = shown(call.Proof.Detail, textutil.PreviewIdentity)
	var b strings.Builder
	fmt.Fprintf(&b, "blocked: Plan mode is still planning, and this %q call was refused", name)
	if s := shown(call.Proof.Subject, textutil.PreviewLocator); s != "" {
		fmt.Fprintf(&b, ": %s", s)
	}
	why := call.Proof.Why
	if why == "" {
		why = "unproven"
	}
	fmt.Fprintf(&b, "\nCause (%s): %s", why, causeLine(name, call.Proof))
	b.WriteString("\nThis is a workflow phase, not a permission decision. ")
	b.WriteString(planningExit)
	return b.String()
}
