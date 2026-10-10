package hook

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"reasonix/internal/base/secrets"
)

// A hook that cannot be evaluated is unknown, not passed. On a gating event
// that unknown stops the action; on an observing event it is only reported.
var (
	ErrInvalidMatcher        = errors.New("hook matcher is not a valid regular expression")
	ErrSpawnFailed           = errors.New("hook process could not be started")
	ErrPayloadUnserializable = errors.New("hook payload could not be serialized")
	ErrApprovalChanged       = errors.New("project hook changed after it was approved")
)

// Stable codes the model and frontends cite for each cause.
const (
	CodeInvalidMatcher        = "invalid_matcher"
	CodeSpawnFailed           = "spawn_failed"
	CodePayloadUnserializable = "payload_unserializable"
	CodeApprovalChanged       = "approval_changed"
)

// UnevaluableCode names the cause an outcome could not be evaluated for, or ""
// when err is none of the unevaluable sentinels.
func UnevaluableCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidMatcher):
		return CodeInvalidMatcher
	case errors.Is(err, ErrSpawnFailed):
		return CodeSpawnFailed
	case errors.Is(err, ErrPayloadUnserializable):
		return CodePayloadUnserializable
	case errors.Is(err, ErrApprovalChanged):
		return CodeApprovalChanged
	}
	return ""
}

func unevaluableStep(err error) string {
	switch UnevaluableCode(err) {
	case CodeInvalidMatcher:
		return "match"
	case CodeSpawnFailed:
		return "spawn"
	case CodeApprovalChanged:
		return "approval"
	default:
		return "payload"
	}
}

// gates reports whether h's verdict decides if the action happens.
func gates(h ResolvedHook) bool { return IsBlocking(h.Event) || claudePermissionBlocking(h) }

// matchTool is the matcher evaluation, with the reason a matcher could not be evaluated.
func matchTool(h ResolvedHook, toolName string) (bool, error) {
	if !UsesToolMatcher(h.Event) {
		return true, nil
	}
	m := h.Match
	if m == "" || m == "*" {
		return true, nil
	}
	re, err := regexp.Compile("^(?:" + m + ")$")
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidMatcher, err)
	}
	if h.PayloadFormat != "claude" {
		return re.MatchString(toolName), nil
	}
	return slices.ContainsFunc(claudeMatchNames(toolName), re.MatchString), nil
}

// recordUnevaluable adds the outcome for a hook that could not be evaluated and
// reports whether it blocks.
func recordUnevaluable(report *Report, h ResolvedHook, cause error) bool {
	decision := DecisionError
	if gates(h) {
		decision = DecisionBlock
		report.Blocked = true
	}
	report.Outcomes = append(report.Outcomes, Outcome{
		Hook: h, Decision: decision, ExitCode: -1, Stderr: cause.Error(), Cause: cause,
	})
	return decision == DecisionBlock
}

// unevaluableReason is what the model is told: identity and structured facts
// only, never the hook's own output or the underlying error prose.
func unevaluableReason(o Outcome) string {
	fields := []string{
		"hook_unevaluable",
		"code=" + UnevaluableCode(o.Cause),
		"step=" + unevaluableStep(o.Cause),
		"event=" + string(o.Hook.Event),
		"scope=" + string(o.Hook.Scope),
	}
	if src := strings.TrimSpace(o.Hook.Source); src != "" {
		fields = append(fields, fmt.Sprintf("source=%q", clipRunes(secrets.Redact(src), 200)))
	}
	if d := strings.TrimSpace(o.Hook.Description); d != "" {
		fields = append(fields, fmt.Sprintf("hook=%q", clipRunes(secrets.Redact(d), 80)))
	}
	if UnevaluableCode(o.Cause) == CodeInvalidMatcher {
		fields = append(fields, fmt.Sprintf("match=%q", clipRunes(secrets.Redact(o.Hook.Match), 80)))
	}
	return strings.Join(fields, " ") + " — the hook could not be evaluated, so this action was not performed; the user must fix or remove the hook"
}

func spawnCause(r SpawnResult) error {
	if r.SpawnErr == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrSpawnFailed, r.SpawnErr)
}
