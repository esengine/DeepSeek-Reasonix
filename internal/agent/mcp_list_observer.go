package agent

import (
	"encoding/json"

	"reasonix/internal/event"
)

type mcpListObservation struct {
	Server      string `json:"server"`
	Source      string `json:"source"`
	Trigger     string `json:"trigger"`
	DurationMs  int64  `json:"duration_ms"`
	ToolCount   int    `json:"tool_count"`
	SchemaBytes int    `json:"schema_bytes"`
	NetworkCall bool   `json:"network_call"`
}

type mcpListObserverBinder interface {
	bindMCPListObserver(func(mcpListObservation))
}

type mcpListObserverActivator interface {
	activateMCPListObserver() func()
}

func (a *Agent) bindCapabilityObservers() {
	a.bindToolResultSessionCapability()
	a.bindMCPListObserverCapability()
}

func (a *Agent) activateMCPListObserver() func() {
	if a == nil || a.svc.tools == nil {
		return func() {}
	}
	proxy, ok := a.svc.tools.Get("use_capability")
	if !ok {
		return func() {}
	}
	activator, ok := proxy.(mcpListObserverActivator)
	if !ok {
		return func() {}
	}
	return activator.activateMCPListObserver()
}

func (a *Agent) bindMCPListObserverCapability() {
	if a == nil || a.svc.tools == nil {
		return
	}
	proxy, ok := a.svc.tools.Get("use_capability")
	if !ok {
		return
	}
	binder, ok := proxy.(mcpListObserverBinder)
	if !ok {
		return
	}
	binder.bindMCPListObserver(func(observation mcpListObservation) {
		detail, _ := json.Marshal(observation)
		a.svc.sink.Emit(event.Event{
			Kind: event.Notice, Level: event.LevelInfo,
			Code: event.NoticeCodeMCPToolsList, Text: "MCP tools/list",
			Detail: string(detail),
		})
	})
}

func (t *UseCapabilityTool) bindMCPListObserver(observer func(mcpListObservation)) {
	if t == nil {
		return
	}
	t.mcpListMu.Lock()
	t.mcpListObserver = observer
	t.mcpListMu.Unlock()
}

func (t *UseCapabilityTool) observeMCPList(observation mcpListObservation) {
	t.mcpListMu.RLock()
	observer := t.mcpListObserver
	t.mcpListMu.RUnlock()
	if observer != nil {
		observer(observation)
	}
}

func (t *UseCapabilityTool) activateMCPListObserver() func() {
	if t == nil || t.runtime == nil {
		return func() {}
	}
	return t.runtime.activateFrontend(t)
}
