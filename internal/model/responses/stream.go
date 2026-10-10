// stream.go — reading one turn out of the Responses event stream.
package responses

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/provider"
)

// streamedCall accumulates one function call across its argument deltas.
type streamedCall struct {
	id, name, arguments string
	argChars            int
	completed           bool
	announced           bool
}

// turn is what a response's events add up to: the visible answer, the reasoning
// beside it, the calls it issued, and the terminal facts the client keeps once
// the stream ends.
type turn struct {
	c   *client
	out chan<- provider.Chunk

	calls     map[string]*streamedCall
	callOrder []string
	// A part can arrive as deltas or as one done event. The seen sets are what
	// stop a server sending both from writing the same text twice.
	seenText, seenRefusal, seenReasoning map[string]bool
	seenSearch                           map[string]struct{}

	items           []json.RawMessage
	text, reasoning strings.Builder
	reasoningID     string
	reasoningStatus string
	responseID      string
	terminal        bool
	failed          bool
}

func newTurn(c *client, out chan<- provider.Chunk) *turn {
	return &turn{
		c: c, out: out,
		calls:         map[string]*streamedCall{},
		seenText:      map[string]bool{},
		seenRefusal:   map[string]bool{},
		seenReasoning: map[string]bool{},
		seenSearch:    map[string]struct{}{},
	}
}

func (t *turn) send(ctx context.Context, chunk provider.Chunk) bool {
	return sendChunk(ctx, t.out, chunk)
}

func (t *turn) callFor(itemID string) *streamedCall {
	if call := t.calls[itemID]; call != nil {
		return call
	}
	call := &streamedCall{id: itemID}
	t.calls[itemID] = call
	t.callOrder = append(t.callOrder, itemID)
	return call
}

// apply folds one event in. False means the consumer is gone and the read
// should stop; it does not mean the event went unrecognised.
func (t *turn) apply(ctx context.Context, event sseEvent) bool {
	key := fmt.Sprintf("%s:%d", event.ItemID, event.ContentIndex)
	return t.applyContent(ctx, event, key) && t.applyItem(ctx, event) && t.applyTerminal(ctx, event)
}

// applyContent handles the parts a person reads: the answer, a refusal in its
// place, the thinking beside it, and the protocol's own stream error.
func (t *turn) applyContent(ctx context.Context, event sseEvent, key string) bool {
	switch event.Type {
	case "response.output_text.delta":
		t.seenText[key] = true
		return t.write(ctx, &t.text, provider.ChunkText, event.Delta)
	case "response.output_text.done":
		if event.Text == "" || t.seenText[key] {
			return true
		}
		return t.write(ctx, &t.text, provider.ChunkText, event.Text)
	case "response.refusal.delta":
		// A refusal is this turn's visible answer. Dropping it streams an empty
		// reply, which the agent then retries into the same refusal.
		t.seenRefusal[key] = true
		return t.write(ctx, &t.text, provider.ChunkText, event.Delta)
	case "response.refusal.done":
		if event.Refusal == "" || t.seenRefusal[key] {
			return true
		}
		return t.write(ctx, &t.text, provider.ChunkText, event.Refusal)
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		t.seenReasoning[key] = true
		return t.write(ctx, &t.reasoning, provider.ChunkReasoning, event.Delta)
	case "response.reasoning_text.done", "response.reasoning_summary_text.done":
		if event.Text == "" || t.seenReasoning[key] {
			return true
		}
		return t.write(ctx, &t.reasoning, provider.ChunkReasoning, event.Text)
	case "error":
		return t.streamError(ctx, event)
	}
	return true
}

// streamError reports the protocol's own error event, which is not a response
// event: it carries its code and message directly and ends the turn.
func (t *turn) streamError(ctx context.Context, event sseEvent) bool {
	t.terminal, t.failed = true, true
	err := error(&provider.StreamPayloadError{Provider: t.c.name, Message: cmp.Or(strings.TrimSpace(event.Message), "stream error"), Code: event.Code})
	if authErr := authErrorFromResponse(t.c, &sseError{Message: event.Message, Code: event.Code}); authErr != nil {
		err = authErr
	}
	return t.send(ctx, provider.Chunk{Type: provider.ChunkError, Err: err})
}

func (t *turn) write(ctx context.Context, into *strings.Builder, kind provider.ChunkType, s string) bool {
	into.WriteString(s)
	return t.send(ctx, provider.Chunk{Type: kind, Text: s})
}

// applyItem handles the output items: tool calls as they stream, and the
// server-run searches that arrive already finished.
func (t *turn) applyItem(ctx context.Context, event sseEvent) bool {
	switch event.Type {
	case "response.output_item.added":
		return t.beginItem(ctx, event.Item)
	case "response.function_call_arguments.delta":
		call := t.callFor(event.ItemID)
		call.arguments += event.Delta
		call.argChars += len(event.Delta)
		return t.send(ctx, provider.Chunk{
			Type:     provider.ChunkToolCallArgsDelta,
			ToolCall: &provider.ToolCall{ID: call.id, Name: call.name}, ArgChars: call.argChars,
		})
	case "response.function_call_arguments.done":
		call := t.callFor(event.ItemID)
		if event.Arguments != "" {
			call.arguments = event.Arguments
		}
		return t.completeCall(ctx, call)
	case "response.output_item.done":
		return t.finishItem(ctx, event.Item)
	}
	return true
}

func (t *turn) beginItem(ctx context.Context, item *sseItem) bool {
	if item == nil {
		return true
	}
	switch item.Type {
	case "function_call":
		call := t.callFor(item.ID)
		call.id, call.name = item.CallID, item.Name
		call.announced = true
		return t.send(ctx, provider.Chunk{
			Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: call.id, Name: call.name},
		})
	case "reasoning":
		// 多段推理（DeepSeek 长思考分多段）时末段 id 覆盖：round-trip 合并为
		// 一个 reasoning item 只带末段 id（服务端接受）。
		if item.ID != "" {
			t.reasoningID = item.ID
		}
	}
	return true
}

func (t *turn) finishItem(ctx context.Context, item *sseItem) bool {
	if item == nil {
		return true
	}
	switch item.Type {
	case "web_search_call":
		return t.searchItem(ctx, item)
	case "function_call":
		call := t.callFor(item.ID)
		call.id = cmp.Or(item.CallID, call.id)
		call.name = cmp.Or(item.Name, call.name)
		call.arguments = cmp.Or(item.Arguments, call.arguments)
		return t.completeCall(ctx, call)
	case "reasoning":
		// The done event carries the item's final status, which the next turn's
		// input reasoning item round-trips.
		if item.Status != "" {
			t.reasoningStatus = item.Status
		}
	}
	return true
}

// completeCall dispatches a call once. One with no name yet, such as an
// arguments.done event that named no item, stays open for a later event to name.
func (t *turn) completeCall(ctx context.Context, call *streamedCall) bool {
	if call.completed || call.name == "" {
		return true
	}
	call.arguments = cmp.Or(call.arguments, "{}")
	call.completed = true
	return t.send(ctx, provider.Chunk{
		Type:     provider.ChunkToolCall,
		ToolCall: &provider.ToolCall{ID: call.id, Name: call.name, Arguments: call.arguments},
	})
}

// searchItem keeps a completed search twice over: as the opaque item a
// stateless follow-up replays, and as the finished server tool a card draws.
func (t *turn) searchItem(ctx context.Context, item *sseItem) bool {
	if !t.c.webSearch {
		return true
	}
	if _, ok := decodeReplayableWebSearchItem(item.Raw); !ok {
		return true
	}
	key := cmp.Or(item.ID, string(item.Raw))
	if _, seen := t.seenSearch[key]; seen {
		return true
	}
	t.seenSearch[key] = struct{}{}
	raw := append(json.RawMessage(nil), item.Raw...)
	t.items = append(t.items, raw)
	if !t.send(ctx, provider.Chunk{Type: provider.ChunkResponsesItem, ResponsesItem: raw}) {
		return false
	}
	card, ok := searchCallChunk(raw)
	return !ok || t.send(ctx, card)
}

func (t *turn) applyTerminal(ctx context.Context, event sseEvent) bool {
	switch event.Type {
	case "response.completed", "response.incomplete", "response.failed":
	default:
		return true
	}
	t.terminal = true
	if event.Response == nil {
		if event.Type == "response.completed" {
			if err := t.pendingCallError(); err != nil {
				return t.refuseCalls(ctx, err)
			}
		}
		return true
	}
	if !t.reportUsage(ctx, event) {
		return false
	}
	if event.Type == "response.completed" {
		if err := t.terminalOutputError(event.Response.Output); err != nil {
			return t.refuseCalls(ctx, err)
		}
		t.responseID = event.Response.ID
	}
	if event.Type != "response.failed" && !t.closeCallsFromOutput(ctx, event.Response.Output) {
		return false
	}
	if event.Type == "response.completed" {
		if err := t.pendingCallError(); err != nil {
			return t.refuseCalls(ctx, err)
		}
	}
	if event.Type != "response.failed" {
		return true
	}
	t.failed = true
	return t.send(ctx, provider.Chunk{Type: provider.ChunkError, Err: failureError(t.c, event.Response.Error)})
}

// closeCallsFromOutput recovers omitted done events, deduplicated by call ID.
// Completed output is validated first; incomplete output may still contain
// truncated or unreadable calls that must remain undispatched.
func (t *turn) closeCallsFromOutput(ctx context.Context, output []json.RawMessage) bool {
	for _, raw := range output {
		var item sseItem
		if json.Unmarshal(raw, &item) != nil || item.Type != "function_call" || item.CallID == "" ||
			(item.Status != "" && item.Status != "completed") || t.callClosed(item.CallID) {
			continue
		}
		if !t.finishItem(ctx, &item) {
			return false
		}
	}
	return true
}

func (t *turn) callClosed(callID string) bool {
	for _, call := range t.calls {
		if call.completed && call.id == callID {
			return true
		}
	}
	return false
}

func failureError(c *client, responseError *sseError) error {
	if responseError == nil {
		return fmt.Errorf("responses: response failed")
	}
	if authErr := authErrorFromResponse(c, responseError); authErr != nil {
		return authErr
	}
	return &provider.StreamPayloadError{Provider: c.name, Message: responseError.Message, Code: responseError.Code}
}

// reportUsage forwards the turn's accounting. An all-zero usage object still
// reports when a finish reason came with it: DashScope leaves the numbers empty
// on a reporting gap, and the agent needs the object to know the turn finished.
func (t *turn) reportUsage(ctx context.Context, event sseEvent) bool {
	usage := usageFromResponse(event.Response)
	provider.ApplyRequestAttemptCount(ctx, usage)
	switch {
	case event.Type == "response.incomplete":
		usage.FinishReason = incompleteFinishReason(event.Response.IncompleteDetails.Reason)
	case event.Type == "response.completed" && usage.FinishReason == "":
		usage.FinishReason = "stop"
	}
	if usage.TotalTokens == 0 && usage.FinishReason == "" {
		return true
	}
	return t.send(ctx, provider.Chunk{Type: provider.ChunkUsage, Usage: usage})
}

func incompleteFinishReason(reason string) string {
	switch reason {
	case "max_output_tokens":
		return "length"
	case "content_filter":
		return "content_filter"
	}
	return "incomplete"
}

// assistant is the turn as it will be replayed: the message a stateful digest is
// taken over and a stateless follow-up sends back.
func (t *turn) assistant() provider.Message {
	message := provider.Message{
		Role: provider.RoleAssistant, Content: t.text.String(), ReasoningContent: t.reasoning.String(),
		ReasoningID: t.reasoningID, ReasoningStatus: t.reasoningStatus, ResponsesItems: t.items,
	}
	for _, itemID := range t.callOrder {
		if call := t.calls[itemID]; call.completed {
			message.ToolCalls = append(message.ToolCalls, provider.ToolCall{ID: call.id, Name: call.name, Arguments: call.arguments})
		}
	}
	return message
}

// finish records the continuation point and closes the turn out.
func (t *turn) finish(ctx context.Context, requestMessages []provider.Message) {
	if t.responseID != "" {
		expected := append(append([]provider.Message(nil), requestMessages...), t.assistant())
		t.c.mu.Lock()
		if t.c.effectiveModeLocked() == "stateful" {
			t.c.lastResponseID = t.responseID
			t.c.expectedPrefixDigest = t.c.conversationDigest(expected)
		}
		t.c.mu.Unlock()
	} else {
		t.c.ResetContext()
	}
	if t.failed {
		return
	}
	// reasoning 的 id/status 作为元数据 chunk（空 Text）流给 Agent，持久化进
	// session 后下一轮 input reasoning item 回传。
	if t.reasoningID != "" || t.reasoningStatus != "" {
		if !t.send(ctx, provider.Chunk{Type: provider.ChunkReasoning, ReasoningID: t.reasoningID, ReasoningStatus: t.reasoningStatus}) {
			return
		}
	}
	_ = t.send(ctx, provider.Chunk{Type: provider.ChunkDone})
}

func (c *client) readStream(ctx context.Context, resp *http.Response, out chan<- provider.Chunk, requestMessages []provider.Message, downgrade bool) {
	defer resp.Body.Close()
	defer close(out)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	idle := c.idleTimeout
	if idle <= 0 {
		idle = defaultStreamIdleTimeout
	}
	watchDone := make(chan struct{})
	activity := make(chan struct{}, 1)
	var stalled atomic.Bool
	go watchIdle(ctx, resp.Body, idle, activity, watchDone, &stalled)
	defer close(watchDone)

	t := newTurn(c, out)
	completed := false
	for scanner.Scan() {
		select {
		case activity <- struct{}{}:
		default:
		}
		data, ok := sseData(scanner.Text())
		if !ok {
			continue
		}
		if data == "[DONE]" {
			if err := t.pendingCallError(); err != nil {
				t.refuseCalls(ctx, err)
				return
			}
			t.terminal = true
			break
		}
		if err := doneArgumentsError([]byte(data)); err != nil {
			t.refuseCalls(ctx, err)
			return
		}
		var event sseEvent
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		if !t.apply(ctx, event) {
			return
		}
		completed = event.Type == "response.completed" && event.Response != nil
		if t.terminal {
			break
		}
	}

	if ctx.Err() != nil {
		return
	}
	if err := scanner.Err(); err != nil {
		_ = sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: interruptedRead(err, idle, stalled.Load())})
		return
	}
	// Protocol-defined terminal response events are required. Connection close
	// before one leaves the attempt uncommitted — including any complete tool
	// calls already forwarded as speculative output.
	if !t.terminal {
		_ = sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkError, Err: provider.StreamInterrupt(io.ErrUnexpectedEOF, provider.StreamInterruptPrematureEOF)})
		return
	}
	if downgrade && completed && !t.failed {
		c.disableContinuation()
	}
	t.finish(ctx, requestMessages)
}

func sseData(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "data:")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func interruptedRead(err error, idle time.Duration, stalled bool) error {
	if stalled {
		return provider.StreamInterrupt(fmt.Errorf("responses: stream idle timeout after %s; raise idle_timeout_seconds to allow a longer silence", idle), provider.StreamInterruptIdleTimeout)
	}
	return provider.StreamInterrupt(err, provider.ClassifyStreamInterrupt(err))
}

// watchIdle closes the body when nothing has arrived for idle, so a silent
// connection ends as a timeout rather than holding the turn open.
func watchIdle(ctx context.Context, body io.Closer, idle time.Duration, activity, done <-chan struct{}, stalled *atomic.Bool) {
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = body.Close()
			return
		case <-done:
			return
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idle)
		case <-timer.C:
			stalled.Store(true)
			_ = body.Close()
			return
		}
	}
}

func sendChunk(ctx context.Context, out chan<- provider.Chunk, chunk provider.Chunk) bool {
	select {
	case out <- chunk:
		return true
	default:
	}
	notifySendChunkEnterBlocking()
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}
