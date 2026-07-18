package runtimeservice

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/control"
	"reasonix/internal/runtimeapi"
)

// ErrInvalidComposerSubmit means the target-neutral composer union cannot be
// admitted. It is deliberately distinct from an unknown slash command: an
// unknown command is a valid, completed/no-effect submission that produces a
// Notice, while malformed structured input is rejected before any mutation.
var ErrInvalidComposerSubmit = errors.New("runtime service: invalid composer submit")

// ComposerRouteKind is the shared semantic branch selected before either the
// Local adapter or the Remote Host performs target-specific admission.
type ComposerRouteKind string

const (
	ComposerTurn      ComposerRouteKind = "turn"
	ComposerOperation ComposerRouteKind = "operation"
	ComposerLifecycle ComposerRouteKind = "lifecycle"
	ComposerCompleted ComposerRouteKind = "completed"
)

// ComposerTurnKind selects the exact Controller primitive. Keeping this choice
// in RuntimeService prevents Local and Remote from disagreeing about display,
// edited-message, invocation, or delivery-recovery semantics.
type ComposerTurnKind string

const (
	ComposerTurnNormal      ComposerTurnKind = "normal"
	ComposerTurnEdited      ComposerTurnKind = "edited"
	ComposerTurnInvocations ComposerTurnKind = "invocations"
	ComposerTurnRecovery    ComposerTurnKind = "delivery_recovery"
	ComposerTurnRawSlash    ComposerTurnKind = "raw_slash"
)

type ComposerLifecycleKind string

const (
	ComposerLifecycleNew    ComposerLifecycleKind = "new"
	ComposerLifecycleClear  ComposerLifecycleKind = "clear"
	ComposerLifecycleBranch ComposerLifecycleKind = "branch"
	ComposerLifecycleRewind ComposerLifecycleKind = "rewind"
)

// ComposerCompletionKind tells the executor which non-Turn action (if any) is
// still required. ControllerNotice is used only for syntactically read-only or
// unknown commands; Host management writes are never passed to Controller.
type ComposerCompletionKind string

const (
	ComposerCompletionNotice          ComposerCompletionKind = "notice"
	ComposerCompletionMemoryRemember  ComposerCompletionKind = "memory_remember"
	ComposerCompletionGoalStatus      ComposerCompletionKind = "goal_status"
	ComposerCompletionGoalClear       ComposerCompletionKind = "goal_clear"
	ComposerCompletionProfileEffort   ComposerCompletionKind = "profile_effort"
	ComposerCompletionHostWriteDenied ComposerCompletionKind = "host_write_denied"
)

// ComposerInvocationSymbol is the authoritative live invocation catalogue.
// Names are slash names without a leading slash.
type ComposerInvocationSymbol struct {
	Name string
	Kind runtimeapi.InvocationKind
}

// ComposerSymbols contains only identities needed to prove that a raw slash
// or structured invocation really starts a Turn. Bodies, paths, MCP transport
// settings, and credentials never enter the dispatcher.
type ComposerSymbols struct {
	TurnCommands []string
	Invocations  []ComposerInvocationSymbol
}

type ComposerRoute struct {
	Kind       ComposerRouteKind
	Turn       ComposerTurnKind
	Operation  runtimeapi.OperationKind
	Lifecycle  ComposerLifecycleKind
	Completion ComposerCompletionKind
	Effect     runtimeapi.SubmitEffect

	Input            string
	DisplayText      string
	EditedOriginal   string
	Invocations      []runtimeapi.Invocation
	DeliveryRecovery bool
	Argument         string
}

// RouteComposerSubmit applies the frozen raw-composer union and Remote V1
// security boundary without executing work. Executors then perform the chosen
// action under their own Local generation or Remote requestId/epoch barrier.
func RouteComposerSubmit(input runtimeapi.ComposerSubmitInput, symbols ComposerSymbols) (ComposerRoute, error) {
	if !input.Session.Valid() {
		return ComposerRoute{}, invalidComposerSubmit("invalid Session target")
	}
	if !utf8.ValidString(input.Input) || !utf8.ValidString(input.DisplayText) || !utf8.ValidString(input.EditedOriginal) {
		return ComposerRoute{}, invalidComposerSubmit("input is not valid UTF-8")
	}
	if strings.TrimSpace(input.Input) == "" {
		return ComposerRoute{}, invalidComposerSubmit("input is empty")
	}
	if input.DeliveryRecovery && (input.EditedOriginal != "" || len(input.Invocations) != 0) {
		return ComposerRoute{}, invalidComposerSubmit("delivery recovery cannot be combined with edited or invocation input")
	}
	if input.EditedOriginal != "" && len(input.Invocations) != 0 {
		return ComposerRoute{}, invalidComposerSubmit("edited input cannot be combined with invocations")
	}

	display := input.DisplayText
	if display == "" {
		display = input.Input
	}
	base := ComposerRoute{
		Input: input.Input, DisplayText: display, EditedOriginal: input.EditedOriginal,
		Invocations:      append([]runtimeapi.Invocation(nil), input.Invocations...),
		DeliveryRecovery: input.DeliveryRecovery,
	}

	if len(input.Invocations) != 0 {
		if err := validateComposerInvocations(input.Invocations, symbols.Invocations); err != nil {
			return ComposerRoute{}, err
		}
		base.Kind, base.Turn = ComposerTurn, ComposerTurnInvocations
		return base, nil
	}
	if input.DeliveryRecovery {
		base.Kind, base.Turn = ComposerTurn, ComposerTurnRecovery
		return base, nil
	}

	trimmed := strings.TrimSpace(input.Input)
	if note, ok := control.MemoryQuickAddNote(trimmed); ok {
		base.Kind, base.Completion = ComposerCompleted, ComposerCompletionMemoryRemember
		base.Effect, base.Argument = runtimeapi.EffectStateChanged, note
		return base, nil
	}
	if note, ok := control.RememberCommandNote(trimmed); ok {
		if note == "" {
			return completedNotice(base), nil
		}
		base.Kind, base.Completion = ComposerCompleted, ComposerCompletionMemoryRemember
		base.Effect, base.Argument = runtimeapi.EffectStateChanged, note
		return base, nil
	}
	if goal, ok := control.ParseGoalCommand(trimmed); ok {
		switch goal.Action {
		case control.GoalCommandSet:
			// The raw Goal path starts the same goal loop as the existing
			// Controller dispatcher, so it owns a real opaque Turn identity.
			base.Kind, base.Turn = ComposerTurn, ComposerTurnRawSlash
			return base, nil
		case control.GoalCommandClear:
			base.Kind, base.Completion = ComposerCompleted, ComposerCompletionGoalClear
			base.Effect = runtimeapi.EffectStateChanged
			return base, nil
		default:
			base.Kind, base.Completion = ComposerCompleted, ComposerCompletionGoalStatus
			base.Effect = runtimeapi.EffectNone
			return base, nil
		}
	}

	if strings.HasPrefix(trimmed, "!") {
		command := strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
		if command == "" {
			return ComposerRoute{}, invalidComposerSubmit("shell command is empty")
		}
		base.Kind, base.Operation = ComposerOperation, runtimeapi.OperationShell
		base.Argument = command
		return base, nil
	}

	if !strings.HasPrefix(trimmed, "/") || isSlashPromptText(trimmed) {
		base.Kind = ComposerTurn
		if input.EditedOriginal != "" {
			base.Turn = ComposerTurnEdited
		} else {
			base.Turn = ComposerTurnNormal
		}
		return base, nil
	}

	fields := strings.Fields(trimmed)
	verb := strings.ToLower(fields[0])
	switch verb {
	case "/new":
		if len(fields) != 1 {
			return completedNotice(base), nil
		}
		base.Kind, base.Lifecycle = ComposerLifecycle, ComposerLifecycleNew
		base.Effect = runtimeapi.EffectSessionReplaced
		return base, nil
	case "/clear":
		if len(fields) != 1 {
			return completedNotice(base), nil
		}
		base.Kind, base.Lifecycle = ComposerLifecycle, ComposerLifecycleClear
		base.Effect = runtimeapi.EffectSessionReplaced
		return base, nil
	case "/compact":
		base.Kind, base.Operation = ComposerOperation, runtimeapi.OperationCompact
		base.Argument = strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		return base, nil
	case "/branch":
		base.Kind, base.Lifecycle = ComposerLifecycle, ComposerLifecycleBranch
		base.Effect = runtimeapi.EffectSessionReplaced
		base.Argument = strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		return base, nil
	case "/rewind":
		base.Kind, base.Lifecycle = ComposerLifecycle, ComposerLifecycleRewind
		base.Effect = runtimeapi.EffectRuntimeReplaced
		base.Argument = strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		return base, nil
	case "/effort":
		base.Kind, base.Completion = ComposerCompleted, ComposerCompletionProfileEffort
		base.Effect = runtimeapi.EffectStateChanged
		base.Argument = strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		return base, nil
	case "/plan-exec", "/prometheus":
		base.Kind, base.Turn = ComposerTurn, ComposerTurnRawSlash
		return base, nil
	}

	if hostManagementWrite(trimmed) {
		base.Kind, base.Completion = ComposerCompleted, ComposerCompletionHostWriteDenied
		base.Effect = runtimeapi.EffectNone
		return base, nil
	}
	if rawSlashStartsTurn(fields[0], symbols) {
		base.Kind, base.Turn = ComposerTurn, ComposerTurnRawSlash
		return base, nil
	}
	return completedNotice(base), nil
}

func validateComposerInvocations(input []runtimeapi.Invocation, symbols []ComposerInvocationSymbol) error {
	available := make(map[string]runtimeapi.InvocationKind, len(symbols))
	for _, symbol := range symbols {
		name := strings.TrimPrefix(strings.TrimSpace(symbol.Name), "/")
		if name == "" || (symbol.Kind != runtimeapi.InvocationSkill && symbol.Kind != runtimeapi.InvocationSubagent) {
			continue
		}
		available[name] = symbol.Kind
	}
	for index, invocation := range input {
		name := strings.TrimPrefix(strings.TrimSpace(invocation.Name), "/")
		if name == "" || invocation.Name != name ||
			(invocation.Kind != runtimeapi.InvocationSkill && invocation.Kind != runtimeapi.InvocationSubagent) {
			return invalidComposerSubmit("invocation %d is malformed", index)
		}
		kind, ok := available[name]
		if !ok || kind != invocation.Kind {
			return invalidComposerSubmit("invocation %d is unavailable or changed kind", index)
		}
	}
	return nil
}

func isSlashPromptText(trimmed string) bool {
	if control.SlashCodeCommentLine(trimmed) || control.SlashPathLikeLine(trimmed) {
		return true
	}
	_, existingFile := control.FileRefLine(trimmed)
	return existingFile
}

func rawSlashStartsTurn(token string, symbols ComposerSymbols) bool {
	name := strings.TrimPrefix(strings.TrimSpace(token), "/")
	if name == "" {
		return false
	}
	for _, command := range symbols.TurnCommands {
		if strings.TrimPrefix(strings.TrimSpace(command), "/") == name {
			return true
		}
	}
	for _, invocation := range symbols.Invocations {
		if strings.TrimPrefix(strings.TrimSpace(invocation.Name), "/") == name {
			return true
		}
	}
	return strings.HasPrefix(name, "mcp__") && containsSymbol(symbols.TurnCommands, name)
}

func containsSymbol(values []string, name string) bool {
	for _, value := range values {
		if strings.TrimPrefix(strings.TrimSpace(value), "/") == name {
			return true
		}
	}
	return false
}

func hostManagementWrite(input string) bool {
	fields := strings.Fields(strings.ToLower(input))
	if len(fields) == 0 {
		return false
	}
	sub := ""
	if len(fields) > 1 {
		sub = fields[1]
	}
	switch fields[0] {
	case "/migrate", "/migration", "/reload-cmd", "/switch":
		return true
	case "/skill", "/skills":
		return sub == "enable" || sub == "disable"
	case "/hooks":
		return sub == "trust"
	case "/mcp":
		return sub == "connect" || sub == "disconnect" || sub == "remove" || sub == "add"
	case "/memory-v5":
		return sub != "" && sub != "status" && sub != "learnings"
	case "/plugin", "/plugins":
		return sub == "install" || sub == "uninstall" || sub == "enable" || sub == "disable"
	default:
		return false
	}
}

func completedNotice(base ComposerRoute) ComposerRoute {
	base.Kind, base.Completion = ComposerCompleted, ComposerCompletionNotice
	base.Effect = runtimeapi.EffectNone
	return base
}

func invalidComposerSubmit(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidComposerSubmit, fmt.Sprintf(format, args...))
}
