package agent

import (
	"fmt"
	"math"
	"sync/atomic"
	"unicode/utf8"

	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

const outputBudgetReserve = 8 * 1024

type outputBudgetState struct {
	outputBudget      int
	activeReqShape    atomic.Pointer[requestCalibrationShape]
	promptCalibration atomic.Pointer[promptTokenCalibration]
}

type promptTokenCalibration struct {
	promptTokens int
	requestChars int64
	compactChars int64
	cjkRunes     int64
	cjkBytes     int64
}

// requestCalibrationShape pairs the conservative provider-visible text and CJK
// composition used for overflow protection with the legacy content-only shape
// used by fold economics. Keeping them in one immutable pointer ensures readers
// never combine calibration fields from different prepared requests.
type requestCalibrationShape struct {
	requestChars int64
	compactChars int64
	cjkRunes     int64
	cjkBytes     int64
}

func (a *Agent) resetOutputBudgetState() {
	a.lastUsage.Store(nil)
	a.activeReqShape.Store(nil)
	a.promptCalibration.Store(nil)
}

func (a *Agent) setPromptTokenCalibration(promptTokens int, shape requestCalibrationShape) {
	if a == nil || promptTokens <= 0 || shape.requestChars <= 0 {
		return
	}
	a.promptCalibration.Store(&promptTokenCalibration{
		promptTokens: promptTokens,
		requestChars: shape.requestChars,
		compactChars: shape.compactChars,
		cjkRunes:     shape.cjkRunes,
		cjkBytes:     shape.cjkBytes,
	})
}

func (a *Agent) setPromptTokenCalibrationFromActive(promptTokens int) {
	if a == nil {
		return
	}
	if shape := a.activeReqShape.Load(); shape != nil {
		a.setPromptTokenCalibration(promptTokens, *shape)
	}
}

// setPromptTokenCalibrationFromUsage trusts provider telemetry only;
// reconstructed usage remains available for accounting but not admission.
func (a *Agent) setPromptTokenCalibrationFromUsage(usage *provider.Usage) {
	if a == nil || usage == nil || usage.Estimated {
		return
	}
	a.setPromptTokenCalibrationFromActive(usage.LatestPromptTokens())
}

func outputBudgetOf(p provider.Provider) int {
	if nilutil.IsNil(p) {
		return 0
	}
	if budget, ok := p.(provider.OutputBudgetProvider); ok {
		return budget.OutputBudget()
	}
	return 0
}

func sharesContextWindow(p provider.Provider) bool {
	if nilutil.IsNil(p) {
		return false
	}
	shared, ok := p.(provider.SharedWindowOutputProvider)
	return ok && shared.SharesContextWindow()
}

func sharedWindowInputPolicyOf(p provider.Provider) provider.SharedWindowInputPolicy {
	if nilutil.IsNil(p) {
		return provider.SharedWindowInputPolicy{}
	}
	policy, ok := p.(provider.SharedWindowInputPolicyProvider)
	if !ok {
		return provider.SharedWindowInputPolicy{}
	}
	return policy.SharedWindowInputPolicy()
}

func (a *Agent) configuredOutputBudget(explicit int) int {
	if explicit != 0 {
		return explicit
	}
	return a.outputBudget
}

func requestCalibrationShapeOf(req provider.Request) requestCalibrationShape {
	return requestCalibrationShapeWithPolicy(req, provider.SharedWindowInputPolicy{})
}

func (a *Agent) requestCalibrationShape(req provider.Request) requestCalibrationShape {
	return requestCalibrationShapeWithPolicy(req, sharedWindowInputPolicyOf(a.prov))
}

func requestCalibrationShapeWithPolicy(req provider.Request, policy provider.SharedWindowInputPolicy) requestCalibrationShape {
	requestChars, cjkRunes, cjkBytes := requestCalibrationTextShape(req, policy)
	return requestCalibrationShape{
		requestChars: requestChars,
		compactChars: int64(charsOfMessages(req.Messages)),
		cjkRunes:     cjkRunes,
		cjkBytes:     cjkBytes,
	}
}

// requestCalibrationTextShape counts common shared-window text plus only the
// adapter-specific replay fields declared by the active provider. This keeps
// omitted bytes out of the ratio without missing newly appended wire content.
func requestCalibrationTextShape(req provider.Request, policy provider.SharedWindowInputPolicy) (chars, cjkRunes, cjkBytes int64) {
	add := func(s string) {
		chars += int64(len(s))
		for _, r := range s {
			if isCJKRune(r) {
				cjkRunes++
				cjkBytes += int64(utf8.RuneLen(r))
			}
		}
	}
	for _, msg := range req.Messages {
		if msg.LocalOnly {
			continue
		}
		chars += 4
		add(string(msg.Role))
		add(msg.Content)
		if msg.Role == provider.RoleAssistant && (len(msg.ToolCalls) > 0 || policy.ReplaysOrdinaryReasoning) {
			add(msg.ReasoningContent)
		}
		add(msg.Name)
		add(msg.ToolCallID)
		for _, call := range msg.ToolCalls {
			chars += 8
			add(call.ID)
			add(call.Name)
			add(call.Arguments)
		}
		if policy.ReplaysResponsesItems {
			for _, item := range msg.ResponsesItems {
				add(string(item))
			}
		}
	}
	for _, schema := range req.Tools {
		chars += 8
		add(schema.Name)
		add(schema.Description)
		add(string(schema.Parameters))
	}
	return chars, cjkRunes, cjkBytes
}

func (a *Agent) calibratedPromptTokens(shape requestCalibrationShape) (int, bool) {
	if shape.requestChars <= 0 {
		return 0, false
	}
	if cal := a.promptCalibration.Load(); cal != nil && cal.requestChars > 0 {
		ratio := float64(cal.promptTokens) / float64(cal.requestChars)
		if ratio > 0.05 && ratio < 2 {
			trustedChars := shape.requestChars
			excessCJKRunes := int64(0)
			// A higher CJK share cannot safely reuse the aggregate ratio. Scale its
			// represented share and apply the cold two-token floor only to excess,
			// preserving exact calibration for stable CJK sessions.
			if shape.cjkRunes*cal.requestChars > cal.cjkRunes*shape.requestChars {
				trustedCJKBytes := cal.cjkBytes * shape.requestChars / cal.requestChars
				trustedCJKRunes := cal.cjkRunes * shape.requestChars / cal.requestChars
				trustedCJKBytes = min(trustedCJKBytes, shape.cjkBytes)
				trustedCJKRunes = min(trustedCJKRunes, shape.cjkRunes)
				trustedChars -= shape.cjkBytes - trustedCJKBytes
				excessCJKRunes = shape.cjkRunes - trustedCJKRunes
			}
			return int(math.Ceil(float64(trustedChars)*ratio)) + int(2*excessCJKRunes), true
		}
	}
	return 0, false
}

// estimatedPromptTokens sizes the final provider-visible messages for overflow
// protection. Real same-session usage calibrates the estimate; before that,
// CJK text gets a conservative second token per rune to cover the measured
// tokenizer gap that can otherwise postpone compaction past the hard limit.
func (a *Agent) estimatedPromptTokens(msgs []provider.Message) int {
	est := estimateMessagesTokens(provider.ModelMessages(msgs))
	if est <= 0 {
		return 0
	}
	if calibrated, ok := a.calibratedPromptTokens(a.requestCalibrationShape(provider.Request{Messages: msgs})); ok {
		return calibrated
	}
	return est + cjkRunesInMessages(msgs)
}

func (a *Agent) estimatedRequestTokens(req provider.Request) int {
	if calibrated, ok := a.calibratedPromptTokens(a.requestCalibrationShape(req)); ok {
		return calibrated
	}
	return a.estimatedPromptTokens(req.Messages) + estimateToolSchemaTokens(req.Tools)
}

func cjkRunesInMessages(msgs []provider.Message) int {
	total := 0
	for _, msg := range msgs {
		if msg.LocalOnly {
			continue
		}
		total += cjkRunesIn(msg.Content) + cjkRunesIn(msg.ReasoningContent)
		total += cjkRunesIn(msg.Name) + cjkRunesIn(msg.ToolCallID)
		for _, call := range msg.ToolCalls {
			total += cjkRunesIn(call.ID) + cjkRunesIn(call.Name) + cjkRunesIn(call.Arguments)
		}
		for _, item := range msg.ResponsesItems {
			total += cjkRunesIn(string(item))
		}
	}
	return total
}

func cjkRunesIn(s string) int {
	total := 0
	for _, r := range s {
		if isCJKRune(r) {
			total++
		}
	}
	return total
}

func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

func estimateToolSchemaTokens(schemas []provider.ToolSchema) int {
	total := 0
	for _, schema := range schemas {
		total += 8
		total += estimateTextTokens(schema.Name)
		total += estimateTextTokens(schema.Description)
		total += estimateTextTokens(string(schema.Parameters))
	}
	return total
}

// effectiveOutputBudget returns an explicit smaller output budget only when a
// shared-window request would otherwise exceed the configured context window.
// The final extension-adjusted request is measured, so later payload rewrites
// cannot invalidate the decision. An exhausted window fails locally instead of
// sending a request the provider will reject with HTTP 400.
func (a *Agent) effectiveOutputBudget(req provider.Request) (int, bool, error) {
	if a == nil || a.contextWindow <= 0 || !sharesContextWindow(a.prov) {
		return 0, false, nil
	}
	budget := a.configuredOutputBudget(req.MaxTokens)
	if budget <= 0 {
		return 0, false, nil
	}
	est := a.estimatedRequestTokens(req)
	available := a.contextWindow - est - outputBudgetReserve
	if available <= 0 {
		return 0, false, fmt.Errorf("%w: estimated prompt %d leaves no shared-window output budget", ErrCompactionRequired, est)
	}
	if budget <= available {
		return 0, false, nil
	}
	return available, true, nil
}
