package agent

import (
	"encoding/json"
	"log/slog"
	"time"

	"reasonix/internal/provider"
)

// mainRequestBytes freezes the exact provider-visible byte unit of the last
// sampling request. The server caches system+tools+messages as one prefix, so
// the summarizer replays all three to hit the cached unit; messages alone
// leaves the tools seam unaligned whenever the live tool set changes.
type mainRequestBytes struct {
	messages []provider.Message
	tools    []provider.ToolSchema
}

// saveMainRequest freezes the provider-visible request unit right before send,
// so a later summarizer can reuse that byte prefix instead of the live view
// (which drifts after prune/projection updates) or the live tool set (which
// grows as MCP servers finish registering). Deep-copied: the request payload
// is frozen and must not alias session storage.
func (a *Agent) saveMainRequest(msgs []provider.Message, tools []provider.ToolSchema) {
	cp := make([]provider.Message, len(msgs))
	for i, m := range msgs {
		cp[i] = m
		cp[i].ToolCalls = append([]provider.ToolCall(nil), m.ToolCalls...)
		cp[i].Images = append([]string(nil), m.Images...)
		cp[i].ResponsesItems = append([]json.RawMessage(nil), m.ResponsesItems...)
		cp[i].ServerSearch = append([]provider.ServerSearchCall(nil), m.ServerSearch...)
	}
	var toolCP []provider.ToolSchema
	if len(tools) > 0 {
		toolCP = make([]provider.ToolSchema, len(tools))
		for i, s := range tools {
			toolCP[i] = s
			if len(s.Parameters) > 0 {
				toolCP[i].Parameters = append(json.RawMessage(nil), s.Parameters...)
			}
		}
	}
	a.sess.lastMainReq.Store(&mainRequestBytes{messages: cp, tools: toolCP})
	a.sess.setWireFP(providerVisibleFingerprint(cp))
	a.maybePersistFreshMainRequest()
}

// savedMainRequest returns the frozen bytes of the last sampling request, or
// nil when none was sent in this process (fresh resume included).
func (a *Agent) savedMainRequest() *mainRequestBytes {
	p := a.sess.lastMainReq.Load()
	if p == nil {
		return nil
	}
	return p
}

// freshWireSidecarInterval throttles how often a main request refreshes the
// sidecar's frozen wire bytes. Rewriting the sidecar at per-request frequency
// would make disk IO a per-turn cost; the interval amortizes it while still
// leaving a recently-active session with a recent prefix for the next resume.
const freshWireSidecarInterval = 60 * time.Second

// maybePersistFreshMainRequest refreshes the sidecar's last_wire_* fields with
// the current frozen main-request bytes, throttled. The sidecar otherwise only
// updates on compaction commits, so a long-lived session resumes with a stale
// prefix whose server-side cache has already been evicted (2026-08-31 23:01:
// 0% hit after 15h of activity).
func (a *Agent) maybePersistFreshMainRequest() {
	if a == nil || a.sess.path == "" {
		return
	}
	if time.Since(time.Unix(0, a.sess.lastMainReqPersist.Load())) < freshWireSidecarInterval {
		return
	}
	saved := a.savedMainRequest()
	if saved == nil || len(saved.messages) == 0 {
		return
	}
	a.sess.compactionMu.Lock()
	a.sess.compactionState.LastWireMessages = append([]provider.Message(nil), saved.messages...)
	a.sess.compactionState.LastWireTools = append([]provider.ToolSchema(nil), saved.tools...)
	a.sess.compactionState.UpdatedAt = time.Now().UTC()
	err := a.persistCompactionStateLocked()
	a.sess.compactionMu.Unlock()
	if err != nil {
		slog.Warn("agent: refresh frozen-wire sidecar", "err", err)
		return
	}
	a.sess.lastMainReqPersist.Store(time.Now().UnixNano())
}
