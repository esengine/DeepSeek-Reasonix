package orchestrator

import (
	"fmt"
	"sync"

	"reasonix/internal/event"
)

type SinkMultiplexer struct {
	parentSink event.Sink
	mu         sync.Mutex
	parentID   string
	agentName  string
	verbose    bool
}

func NewSinkMultiplexer(parent event.Sink, name string) *SinkMultiplexer {
	return &SinkMultiplexer{parentSink: parent, agentName: name}
}

func (m *SinkMultiplexer) SetParentID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.parentID = id
}

func (m *SinkMultiplexer) ParentID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.parentID
}

func (m *SinkMultiplexer) SetVerbose(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verbose = v
}

func (m *SinkMultiplexer) Emit(e event.Event) {
	m.mu.Lock()
	pid := m.parentID
	verbose := m.verbose
	m.mu.Unlock()

	switch e.Kind {
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		e.Tool.ParentID = pid
		if e.Kind == event.ToolDispatch && pid != "" {
			e.Tool.ID = pid + "/" + e.Tool.ID
		}
		m.parentSink.Emit(e)

	case event.Usage:
		m.parentSink.Emit(e)

	case event.Message:
		if verbose {
			e.Text = fmt.Sprintf("[%s] %s", m.agentName, e.Text)
			m.parentSink.Emit(e)
		}

	case event.Reasoning:
		if verbose {
			m.parentSink.Emit(e)
		}

	case event.Notice:
		e.Text = fmt.Sprintf("[%s] %s", m.agentName, e.Text)
		m.parentSink.Emit(e)

	case event.Phase:
		e.Text = fmt.Sprintf("%s: %s", m.agentName, e.Text)
		m.parentSink.Emit(e)
	}
}
