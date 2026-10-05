package plugin

import (
	"bytes"
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
)

// stdioReplyQueueBound caps buffered server-request replies. The queue only
// backs up while the reply writer is stuck behind a jammed stdin pipe, so a
// small bound is plenty; overflow drops the reply instead of blocking readLoop.
const stdioReplyQueueBound = 16

// readLoop owns stdout for the transport's lifetime: it reads one JSON-RPC
// message per line, routes progress notifications, answers server requests, and
// hands each response to the call waiting on its id. On any read error it fails
// every pending call and exits.
func (t *stdioTransport) readLoop() {
	// Replies go through replyLoop, never straight to stdin: this is the only
	// reader of stdout, and blocking it on writeMu behind a jammed client write
	// deadlocks both pipes once the server also blocks writing stdout.
	replies := make(chan any, stdioReplyQueueBound)
	defer close(replies)
	go t.replyLoop(replies)
	for {
		line, readErr := t.stdout.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			t.handleInboundLine(line, replies)
		}
		if readErr != nil {
			t.failAll(readErr)
			return
		}
	}
}

// replyLoop serialises server-request replies onto the shared stdin pipe. A
// write failure is not terminal for the transport — the read side may still be
// healthy, and pipe errors surface through the next client call's own write —
// but it stops further replies and keeps draining so readLoop never blocks.
func (t *stdioTransport) replyLoop(replies <-chan any) {
	var dead bool
	for msg := range replies {
		if dead {
			continue
		}
		if t.write(msg) != nil {
			dead = true
		}
	}
}

func (t *stdioTransport) handleInboundLine(line []byte, replies chan<- any) {
	probe, ok := decodeInboundMessage(line)
	if !ok {
		return // unparseable line cannot be routed; keep the transport alive
	}
	if probe.Method != "" {
		if isNotificationID(probe.ID) {
			if probe.Method == "notifications/progress" {
				t.progress.dispatchProgress(probe.Params)
			}
			return
		}
		response := serverRequestReply(probe.ID, probe.Method, t.roots)
		if probe.Method == elicitMethod {
			ctx, e, release := t.elicits.claim()
			if e != nil {
				// Off the read loop, the only reader of stdout: it waits on a person.
				go func() {
					defer release()
					_ = t.write(elicitationReply(ctx, e, t.name, probe.ID, probe.Params))
				}()
				return
			}
			release()
			response = elicitationReply(ctx, nil, t.name, probe.ID, probe.Params)
		}
		select {
		case replies <- response:
		default:
			// The reply writer is stalled behind a full stdin pipe. An
			// unanswered request degrades to the server's own timeout; a
			// blocked readLoop could deadlock both pipes.
		}
		return
	}

	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return
	}
	t.mu.Lock()
	ch := t.pending[resp.ID]
	delete(t.pending, resp.ID)
	t.mu.Unlock()
	if ch != nil {
		ch <- resp // buffered(1): never blocks, even if the caller already left
	}
}

func (t *stdioTransport) registerProgress(token string, sink tool.ProgressFunc) func() {
	return t.progress.registerProgress(token, sink)
}

func (t *stdioTransport) registerElicitCall(ctx context.Context) func() {
	return t.elicits.register(ctx)
}
