package runtimeservice

import (
	"errors"
	"reflect"
	"testing"

	"reasonix/internal/runtimeapi"
)

func composerInput(text string) runtimeapi.ComposerSubmitInput {
	return runtimeapi.ComposerSubmitInput{
		Session: runtimeapi.SessionRef{WorkspaceID: "workspace_1", SessionID: "session_1"},
		Input:   text,
	}
}

func TestRouteComposerSubmitDiscriminatesTurnOperationLifecycleAndCompletion(t *testing.T) {
	symbols := ComposerSymbols{
		TurnCommands: []string{"deploy", "mcp__docs__open"},
		Invocations:  []ComposerInvocationSymbol{{Name: "review", Kind: runtimeapi.InvocationSkill}},
	}
	tests := []struct {
		name       string
		input      string
		kind       ComposerRouteKind
		turn       ComposerTurnKind
		operation  runtimeapi.OperationKind
		lifecycle  ComposerLifecycleKind
		completion ComposerCompletionKind
		effect     runtimeapi.SubmitEffect
		argument   string
	}{
		{name: "normal", input: "explain this", kind: ComposerTurn, turn: ComposerTurnNormal},
		{name: "shell", input: "! git status ", kind: ComposerOperation, operation: runtimeapi.OperationShell, argument: "git status"},
		{name: "compact", input: "/compact keep API details", kind: ComposerOperation, operation: runtimeapi.OperationCompact, argument: "keep API details"},
		{name: "new", input: "/new", kind: ComposerLifecycle, lifecycle: ComposerLifecycleNew, effect: runtimeapi.EffectSessionReplaced},
		{name: "clear", input: "/clear", kind: ComposerLifecycle, lifecycle: ComposerLifecycleClear, effect: runtimeapi.EffectSessionReplaced},
		{name: "custom", input: "/deploy staging", kind: ComposerTurn, turn: ComposerTurnRawSlash},
		{name: "mcp prompt", input: "/mcp__docs__open spec", kind: ComposerTurn, turn: ComposerTurnRawSlash},
		{name: "skill", input: "/review diff", kind: ComposerTurn, turn: ComposerTurnRawSlash},
		{name: "unknown", input: "/does-not-exist", kind: ComposerCompleted, completion: ComposerCompletionNotice, effect: runtimeapi.EffectNone},
		{name: "remember", input: "/remember stable invariant", kind: ComposerCompleted, completion: ComposerCompletionMemoryRemember, effect: runtimeapi.EffectStateChanged, argument: "stable invariant"},
		{name: "goal status", input: "/goal", kind: ComposerCompleted, completion: ComposerCompletionGoalStatus, effect: runtimeapi.EffectNone},
		{name: "goal clear", input: "/goal clear", kind: ComposerCompleted, completion: ComposerCompletionGoalClear, effect: runtimeapi.EffectStateChanged},
		{name: "goal set", input: "/goal --strict finish v1", kind: ComposerTurn, turn: ComposerTurnRawSlash},
		{name: "slash comment", input: "// TODO keep this", kind: ComposerTurn, turn: ComposerTurnNormal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route, err := RouteComposerSubmit(composerInput(test.input), symbols)
			if err != nil {
				t.Fatal(err)
			}
			if route.Kind != test.kind || route.Turn != test.turn || route.Operation != test.operation ||
				route.Lifecycle != test.lifecycle || route.Completion != test.completion || route.Effect != test.effect ||
				route.Argument != test.argument {
				t.Fatalf("route = %+v", route)
			}
		})
	}
}

func TestRouteComposerSubmitPreservesStructuredTurnProjection(t *testing.T) {
	input := composerInput("expanded prompt")
	input.DisplayText = "visible prompt"
	input.Invocations = []runtimeapi.Invocation{
		{Name: "first", Kind: runtimeapi.InvocationSkill},
		{Name: "second", Kind: runtimeapi.InvocationSubagent},
	}
	symbols := ComposerSymbols{Invocations: []ComposerInvocationSymbol{
		{Name: "second", Kind: runtimeapi.InvocationSubagent},
		{Name: "first", Kind: runtimeapi.InvocationSkill},
	}}
	route, err := RouteComposerSubmit(input, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if route.Kind != ComposerTurn || route.Turn != ComposerTurnInvocations || route.DisplayText != "visible prompt" ||
		!reflect.DeepEqual(route.Invocations, input.Invocations) {
		t.Fatalf("route = %+v", route)
	}
	route.Invocations[0].Name = "mutated"
	if input.Invocations[0].Name != "first" {
		t.Fatal("route aliases caller invocation slice")
	}
}

func TestRouteComposerSubmitEditedAndDeliveryUseExactPrimitives(t *testing.T) {
	edited := composerInput("new body")
	edited.DisplayText = "visible"
	edited.EditedOriginal = "old body"
	route, err := RouteComposerSubmit(edited, ComposerSymbols{})
	if err != nil || route.Turn != ComposerTurnEdited {
		t.Fatalf("edited route = %+v, %v", route, err)
	}

	recovery := composerInput("continue")
	recovery.DeliveryRecovery = true
	route, err = RouteComposerSubmit(recovery, ComposerSymbols{})
	if err != nil || route.Turn != ComposerTurnRecovery {
		t.Fatalf("recovery route = %+v, %v", route, err)
	}
}

func TestRouteComposerSubmitBlocksHostManagementWritesBeforeController(t *testing.T) {
	blocked := []string{
		"/hooks trust", "/mcp connect prod", "/skills disable review", "/plugins install demo",
		"/memory-v5 on", "/migrate --from old", "/reload-cmd", "/switch old-branch",
	}
	for _, input := range blocked {
		route, err := RouteComposerSubmit(composerInput(input), ComposerSymbols{})
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if route.Kind != ComposerCompleted || route.Completion != ComposerCompletionHostWriteDenied || route.Effect != runtimeapi.EffectNone {
			t.Fatalf("%q route = %+v", input, route)
		}
	}
	for _, input := range []string{"/hooks", "/mcp", "/skills", "/plugins show demo", "/memory-v5 status"} {
		route, err := RouteComposerSubmit(composerInput(input), ComposerSymbols{})
		if err != nil || route.Completion != ComposerCompletionNotice {
			t.Fatalf("read-only %q route = %+v, %v", input, route, err)
		}
	}
}

func TestRouteComposerSubmitRejectsMalformedUnionWithoutExecutionRoute(t *testing.T) {
	tests := []runtimeapi.ComposerSubmitInput{
		{},
		composerInput("   "),
		func() runtimeapi.ComposerSubmitInput { in := composerInput("!"); return in }(),
		func() runtimeapi.ComposerSubmitInput {
			in := composerInput("x")
			in.DeliveryRecovery = true
			in.EditedOriginal = "old"
			return in
		}(),
		func() runtimeapi.ComposerSubmitInput {
			in := composerInput("x")
			in.Invocations = []runtimeapi.Invocation{{Name: "missing", Kind: runtimeapi.InvocationSkill}}
			return in
		}(),
	}
	for index, input := range tests {
		if _, err := RouteComposerSubmit(input, ComposerSymbols{}); !errors.Is(err, ErrInvalidComposerSubmit) {
			t.Fatalf("case %d error = %v", index, err)
		}
	}
}
