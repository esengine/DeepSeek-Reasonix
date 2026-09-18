package control

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/jobs"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// RunTurn executes one foreground turn synchronously through the same lifecycle
// used by interactive frontends: transient memory/background-job
// composition, checkpoints, hooks, and plan approval. It is for transports that
// need a blocking request/response boundary, such as ACP session/prompt.
func (c *Controller) RunTurn(ctx context.Context, input string) error {
	prepared, failures := c.prepareSubmissionImagesContext(ctx, SubmissionRequest{Input: input})
	if len(failures) > 0 {
		return ImageReferenceFailures(failures)
	}
	ctx = contextWithPreparedImageReferences(ctx, prepared)
	err := c.runSynchronousTurn(ctx, nil, func(runCtx context.Context) error {
		return c.runTurn(runCtx, input)
	})
	if err != nil {
		return err
	}
	return c.waitForGoalTerminal(ctx)
}

// RunSubagentProfile executes one named runAs=subagent skill synchronously and
// returns only its final answer. It is the headless CLI counterpart to explicit
// slash invocation: the child keeps an isolated session, while the caller owns
// stdout rendering and exit status. readOnly selects the preview-safe runner
// used by `reasonix subagent try`.
func (c *Controller) RunSubagentProfile(ctx context.Context, name, task string, readOnly bool) (string, error) {
	ctx = c.withAuthentication(ctx)
	if err := c.authentication.admissionError(); err != nil {
		return "", err
	}
	if _, ok := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences); !ok {
		prepared, failures := c.prepareSubmissionImagesContext(ctx, SubmissionRequest{Input: task})
		if len(failures) > 0 {
			return "", ImageReferenceFailures(failures)
		}
		ctx = contextWithPreparedImageReferences(ctx, prepared)
	}
	name = strings.TrimSpace(name)
	task = strings.TrimSpace(task)
	if name == "" {
		return "", fmt.Errorf("subagent name is required")
	}
	if task == "" {
		return "", fmt.Errorf("subagent task is required")
	}
	sk, ok := c.skills.bySlashName(name)
	if !ok {
		return "", fmt.Errorf("unknown or disabled subagent profile %q", name)
	}
	if sk.RunAs != skill.RunSubagent {
		return "", fmt.Errorf("skill %q is not runAs=subagent", name)
	}
	sk = c.skills.prepare(sk)
	runner := c.skillRunner
	if readOnly {
		runner = c.readOnlySkillRunner
	}
	if runner == nil {
		return "", fmt.Errorf("subagent skill runner is unavailable for %q", name)
	}

	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = c.withTurnImages(ctx, task)
	ctx = agent.WithResponseLanguagePreference(ctx, c.responseLanguage)
	ctx = agent.WithReasoningLanguagePreference(ctx, c.reasoningLanguage)
	ctx = agent.WithSubagentDepth(ctx, 0)
	answer, err := runner(ctx, sk, task, skill.SubagentRunOptions{HostInitiated: true})
	c.authentication.recordFailure(err, c.ModelRef())
	if err != nil {
		return "", err
	}
	return tool.GuardSubagentHostDecisionText(answer), nil
}

// SubmitHTTPFormat is SubmitHTTP with an optional structured-output format
// ("json_object") applied to the turn's completion requests. Empty format
// behaves exactly like SubmitHTTP. A format attached to a slash command,
// or other non-turn input is discarded; @reference turns preserve it because
// the format is bound to every submitted turn rather than a global slot.
func (c *Controller) SubmitHTTPFormat(input, format string) {
	// format 绑定到本次提交的 turn（随请求参数传递），不再写入 Controller
	// 全局一次性槽——评审 #7234 第 2 点：全局槽存在跨请求串用的逻辑竞态
	// （后提交的 JSON 请求先写槽，更早的普通请求先启动消费掉）。
	f := strings.TrimSpace(format)
	if f != "" && isNonTurnHTTPInput(input) {
		f = "" // 非 turn 输入（slash 命令/! 前缀）不携带 format
	}
	// @ 引用 turn（FileRefLine/SlashPathLineRef 等）同样绑定 format——
	// runRefTurnWithFormat 族 wrapper 注入 ctx（review fix7234and7168：
	// format 是每个被接纳 turn 的属性，统一架构）。
	c.submitHTTPWithFormat(input, "", f)
}

// isNonTurnHTTPInput reports inputs that never reach the agent turn loop, so a
// structured-output request attached to them would otherwise leak into the
// next real turn (the format slot is consumed only by runGoalLoopWithRawDisplay).
func isNonTurnHTTPInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return true
	}
	// Memory quick-add / remember shortcuts and goal commands bypass turns.
	if _, ok := MemoryQuickAddNote(trimmed); ok {
		return true
	}
	if _, ok := RememberCommandNote(trimmed); ok {
		return true
	}
	// "!" shell commands are rejected by submitHTTP before the turn loop
	// (403 over HTTP); a format attached to them would never be consumed.
	if strings.HasPrefix(trimmed, "!") {
		return true
	}
	// Slash commands are management verbs (/compact /new /clear /model ...)
	// or notices, not completion turns.
	if strings.HasPrefix(trimmed, "/") {
		return true
	}
	return false
}
