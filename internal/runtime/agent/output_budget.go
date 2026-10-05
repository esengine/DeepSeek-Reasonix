package agent

import (
	"fmt"
	"math"
	"sync/atomic"
	"unicode/utf8"

	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/provider"
)

const outputBudgetReserve = 8 * 1024

type outputBudgetState struct {
	outputBudget int
	// lastUsage caches the latest provider telemetry for per-turn readouts.
	// The run loop writes it while a frontend reads it, so it is atomic.
	lastUsage         atomic.Pointer[provider.Usage]
	activeReqShape    atomic.Pointer[requestCalibrationShape]
	promptCalibration atomic.Pointer[promptTokenCalibration]
	contextUsage      atomic.Pointer[contextUsage] // gauge's memoised prompt size
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
	imageTokens  int64 // estimated, never learned from
}

// resetOutputBudgetState drops what belongs to the transcript being replaced.
// The prompt-token calibration is a property of the model's tokenizer, and a
// model switch rebuilds the agent, so it outlives the swap: dropping it sent
// every rebind — resume, tab switch, recovery adopt — back to the cold
// estimate for a turn.
func (o *outputBudgetState) reset() {
	o.lastUsage.Store(nil)
	o.activeReqShape.Store(nil)
}

// A request that carried images was billed for pixels at a rate the host never
// sees, and a ratio learned from it would price every later character as part
// of a picture.
func (a *contextWindow) setPromptTokenCalibration(promptTokens int, shape requestCalibrationShape) {
	if a == nil || promptTokens <= 0 || shape.requestChars <= 0 || shape.imageTokens > 0 {
		return
	}
	a.sess.output.promptCalibration.Store(&promptTokenCalibration{
		promptTokens: promptTokens,
		requestChars: shape.requestChars,
		compactChars: shape.compactChars,
		cjkRunes:     shape.cjkRunes,
		cjkBytes:     shape.cjkBytes,
	})
}

func (a *contextWindow) setPromptTokenCalibrationFromActive(promptTokens int) {
	if a == nil {
		return
	}
	if shape := a.sess.output.activeReqShape.Load(); shape != nil {
		a.setPromptTokenCalibration(promptTokens, *shape)
	}
}

// setPromptTokenCalibrationFromUsage trusts provider telemetry only, and only
// about our own request: a turn whose provider ran its own tools bills pages we
// never sent, and a ratio learned from it inflates every later estimate.
func (a *contextWindow) setPromptTokenCalibrationFromUsage(usage *provider.Usage) {
	if a == nil || usage == nil || usage.Estimated || usage.ServerToolRequests > 0 {
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

func (a *contextWindow) configuredOutputBudget(explicit int) int {
	if explicit != 0 {
		return explicit
	}
	return a.sess.output.outputBudget
}

func requestCalibrationShapeOf(req provider.Request) requestCalibrationShape {
	return requestCalibrationShapeWithPolicy(req, provider.SharedWindowInputPolicy{})
}

func (a *contextWindow) requestCalibrationShape(req provider.Request) requestCalibrationShape {
	return requestCalibrationShapeWithPolicy(req, sharedWindowInputPolicyOf(a.svc.prov))
}

func requestCalibrationShapeWithPolicy(req provider.Request, policy provider.SharedWindowInputPolicy) requestCalibrationShape {
	var shape requestCalibrationShape
	for _, msg := range req.Messages {
		shape = shape.plus(messageCalibrationShape(msg, policy))
	}
	shape.imageTokens = requestImageTokens(req.Messages)
	for _, schema := range req.Tools {
		shape.requestChars += 8
		shape.addText(schema.Name)
		shape.addText(schema.Description)
		shape.addText(string(schema.Parameters))
	}
	return shape
}

// addText accumulates one wire string's length and CJK composition.
func (s *requestCalibrationShape) addText(str string) {
	s.requestChars += int64(len(str))
	for _, r := range str {
		if isCJKRune(r) {
			s.cjkRunes++
			s.cjkBytes += int64(utf8.RuneLen(r))
		}
	}
}

func (s requestCalibrationShape) plus(o requestCalibrationShape) requestCalibrationShape {
	return requestCalibrationShape{
		requestChars: s.requestChars + o.requestChars,
		compactChars: s.compactChars + o.compactChars,
		cjkRunes:     s.cjkRunes + o.cjkRunes,
		cjkBytes:     s.cjkBytes + o.cjkBytes,
		imageTokens:  s.imageTokens + o.imageTokens,
	}
}

func (s requestCalibrationShape) minus(o requestCalibrationShape) requestCalibrationShape {
	return requestCalibrationShape{
		requestChars: s.requestChars - o.requestChars,
		compactChars: s.compactChars - o.compactChars,
		cjkRunes:     s.cjkRunes - o.cjkRunes,
		cjkBytes:     s.cjkBytes - o.cjkBytes,
		imageTokens:  s.imageTokens - o.imageTokens,
	}
}

// messageCalibrationShape counts one message's shared-window text plus only the
// replay fields the active provider declares, keeping omitted bytes out of the
// ratio. It is additive: requestCalibrationShapeWithPolicy is this sum over a
// request, which is what lets a caller size many suffixes of one transcript
// without rescanning the text once per candidate.
func messageCalibrationShape(msg provider.Message, policy provider.SharedWindowInputPolicy) requestCalibrationShape {
	var shape requestCalibrationShape
	if msg.LocalOnly {
		return shape
	}
	shape.compactChars = int64(msgChars(msg))
	shape.requestChars = 4
	shape.addText(string(msg.Role))
	shape.addText(msg.Content)
	if msg.Role == provider.RoleAssistant && (len(msg.ToolCalls) > 0 || policy.ReplaysOrdinaryReasoning) {
		shape.addText(msg.ReasoningContent)
	}
	shape.addText(msg.Name)
	shape.addText(msg.ToolCallID)
	for _, call := range msg.ToolCalls {
		shape.requestChars += 8
		shape.addText(call.ID)
		shape.addText(call.Name)
		shape.addText(call.Arguments)
	}
	if policy.ReplaysResponsesItems {
		for _, item := range msg.ResponsesItems {
			shape.addText(string(item))
		}
	}
	return shape
}

// projectedMessageCalibrationShape is the shape of the message
// provider.ModelMessages would hand a provider. Only the ProviderContent swap
// and the LocalOnly drop reach the shape, so applying them per message matches
// projecting the whole slice while keeping the caller's indices aligned.
func projectedMessageCalibrationShape(msg provider.Message, policy provider.SharedWindowInputPolicy) requestCalibrationShape {
	if msg.ProviderContent != "" {
		msg.Content = msg.ProviderContent
	}
	return messageCalibrationShape(msg, policy)
}

func (a *contextWindow) calibratedPromptTokens(shape requestCalibrationShape) (int, bool) {
	if shape.requestChars <= 0 {
		return 0, false
	}
	if cal := a.sess.output.promptCalibration.Load(); cal != nil && cal.requestChars > 0 {
		ratio := float64(cal.promptTokens) / float64(cal.requestChars)
		if ratio > 0.05 && ratio < 2 {
			trustedChars := shape.requestChars
			excessCJKBytes := int64(0)
			// A higher CJK share cannot safely reuse the aggregate ratio. Scale its
			// represented share and price only the excess at the cold rate,
			// preserving exact calibration for stable CJK sessions.
			if shape.cjkRunes*cal.requestChars > cal.cjkRunes*shape.requestChars {
				trustedCJKBytes := min(cal.cjkBytes*shape.requestChars/cal.requestChars, shape.cjkBytes)
				excessCJKBytes = shape.cjkBytes - trustedCJKBytes
				trustedChars -= excessCJKBytes
			}
			cold := math.Ceil(float64(excessCJKBytes) * fallbackTokPerChar)
			return int(math.Ceil(float64(trustedChars)*ratio) + cold), true
		}
	}
	return 0, false
}

// estimatedPromptTokens sizes the provider-visible messages in real tokens —
// the only unit comparable against the context window. Same-session usage
// calibrates it; before that the wire character count carries the ~4 chars per
// token shape. estimateMessagesTokens counts characters and is for internal
// planning budgets only; against the window it would compact 4x early.
func (a *contextWindow) estimatedPromptTokens(msgs []provider.Message) int {
	return a.estimatedShapeTokens(a.requestCalibrationShape(provider.Request{Messages: msgs}))
}

func (a *contextWindow) estimatedRequestTokens(req provider.Request) int {
	return a.estimatedShapeTokens(a.requestCalibrationShape(req))
}

func (a *contextWindow) estimatedShapeTokens(shape requestCalibrationShape) int {
	if shape.requestChars <= 0 {
		return 0
	}
	if calibrated, ok := a.calibratedPromptTokens(shape); ok {
		return calibrated + int(shape.imageTokens)
	}
	return int(float64(shape.requestChars)*fallbackTokPerChar) + int(shape.imageTokens)
}

func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

// effectiveOutputBudget clips completion tokens at send time only; it never
// moves compact_ratio. Exhausted windows fail locally before HTTP 400.
func (a *contextWindow) effectiveOutputBudget(req provider.Request) (int, bool, error) {
	window := a.effectiveContextWindow()
	if window <= 0 || !sharesContextWindow(a.svc.prov) {
		return 0, false, nil
	}
	budget := a.configuredOutputBudget(req.MaxTokens)
	if budget <= 0 {
		return 0, false, nil
	}
	est := a.estimatedRequestTokens(req)
	available := window - est - outputBudgetReserve
	if available <= 0 {
		return 0, false, fmt.Errorf("%w: estimated prompt %d leaves no shared-window output budget", ErrCompactionRequired, est)
	}
	if budget <= available {
		return 0, false, nil
	}
	return available, true, nil
}
