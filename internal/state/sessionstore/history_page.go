package sessionstore

import (
	"crypto/rand"
	"errors"
	"sort"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surfacespec"
)

var ErrHistorySnapshotChanged = errors.New("history snapshot changed")
var ErrHistoryPageRange = errors.New("invalid history page range")

type HistoryPageRequest struct {
	Identity string
	Anchor   int
	Before   int
	Limit    int
}

type HistoryToolContext struct {
	MessageIndex int
	Ordinal      int
	Call         provider.ToolCall
}

type HistoryPageRead struct {
	Identity          string
	Version           uint64
	Start, End, Total int
	Messages          []provider.Message
	WorkMs            []int64
	ToolContext       []HistoryToolContext
	TurnStartedAt     int64
}

type historyRun struct {
	turn, run int64
	closing   int
	started   int64
}

type historyReadIndex struct {
	identity string
	version  uint64
	rewrite  int
	calls    map[string]HistoryToolContext
	results  map[int]HistoryToolContext
	outputs  map[string]int
	prefix   []historyRun
	work     []int64
	closedAt []int
}

func newHistoryReadIndex(rewrite int) *historyReadIndex {
	return &historyReadIndex{identity: rand.Text(), rewrite: rewrite, calls: map[string]HistoryToolContext{}, results: map[int]HistoryToolContext{}, outputs: map[string]int{}}
}

func (idx *historyReadIndex) extend(messages []provider.Message, version uint64) {
	for i := len(idx.prefix); i < len(messages); i++ {
		idx.appendMessage(messages[i])
	}
	idx.version = version
}

func (idx *historyReadIndex) appendMessage(m provider.Message) {
	state := historyRun{closing: -1}
	if len(idx.prefix) > 0 {
		state = idx.prefix[len(idx.prefix)-1]
	}
	i := len(idx.prefix)
	visible := !m.LocalOnly || m.Name != surfacespec.ActionMessageName
	idx.work = append(idx.work, 0)
	idx.closedAt = append(idx.closedAt, 0)
	switch m.Role {
	case provider.RoleUser:
		_, _, steer := SteerKind(m.Content)
		if !steer && !m.HostAuthored && !(m.LocalOnly && m.Name == surfacespec.ActionMessageName) {
			state.started = m.CreatedAt
		}
		class := ClassifyTurn(m, 0)
		if !class.Steer {
			state.turn, state.run = state.turn+state.run, 0
			if class.StartsTurn {
				if state.closing >= 0 {
					idx.work[state.closing], idx.closedAt[state.closing] = state.turn, i
				}
				state = historyRun{closing: -1, started: state.started}
			}
		}
	case provider.RoleAssistant:
		if len(m.ToolCalls) == 0 && m.WorkDurationMs > 0 {
			state.run, state.closing = max(state.run, m.WorkDurationMs), i
		}
		for ordinal, call := range m.ToolCalls {
			if visible {
				idx.calls[call.ID] = HistoryToolContext{MessageIndex: i, Ordinal: ordinal, Call: call}
			}
		}
	case provider.RoleTool:
		if visible {
			idx.outputs[m.ToolCallID] = i
		}
		if call, ok := idx.calls[m.ToolCallID]; ok && visible {
			idx.results[i] = call
		}
	}
	idx.prefix = append(idx.prefix, state)
}

// ReadHistoryPage captures range, identity and projection context under one session lock.
func (s *Session) ReadHistoryPage(request HistoryPageRequest) (HistoryPageRead, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := len(s.Messages)
	idx := s.historyIndexLocked()
	return s.historyPageLocked(idx, count, request)
}

func (s *Session) historyIndexLocked() *historyReadIndex {
	count := len(s.Messages)
	idx := s.historyRead
	if idx == nil || idx.rewrite != s.rewriteVersion || len(idx.prefix) > count || s.version-idx.version != uint64(count-len(idx.prefix)) {
		idx = newHistoryReadIndex(s.rewriteVersion)
		s.historyRead = idx
	}
	idx.extend(s.Messages, s.version)
	return idx
}

func (s *Session) ReadHistoryToolOutput(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.historyIndexLocked()
	at, ok := idx.outputs[id]
	if !ok {
		return "", false
	}
	return s.Messages[at].Content, true
}

func (s *Session) historyPageLocked(idx *historyReadIndex, count int, request HistoryPageRequest) (HistoryPageRead, error) {
	if request.Identity != "" && request.Identity != idx.identity {
		return HistoryPageRead{}, ErrHistorySnapshotChanged
	}
	anchor, end := request.Anchor, request.Before
	if request.Identity == "" {
		anchor, end = count, count
	}
	if request.Limit <= 0 || request.Limit > 500 || anchor < 0 || anchor > count || end < 0 || end > anchor {
		return HistoryPageRead{}, ErrHistoryPageRange
	}
	start := max(0, end-request.Limit)
	page := HistoryPageRead{Identity: idx.identity, Version: s.version, Start: start, End: end, Total: anchor,
		Messages: append([]provider.Message{}, s.Messages[start:end]...)}
	if start > 0 {
		page.TurnStartedAt = idx.prefix[start-1].started
	}
	page.WorkMs, page.ToolContext = idx.pageContext(start, end, anchor)
	return page, nil
}

func (idx *historyReadIndex) pageContext(start, end, anchor int) ([]int64, []HistoryToolContext) {
	work := make([]int64, end-start)
	contexts := []HistoryToolContext{}
	seen := map[[2]int]bool{}
	for i := start; i < end; i++ {
		if idx.closedAt[i] > 0 && idx.closedAt[i] < anchor {
			work[i-start] = idx.work[i]
		}
		if call, ok := idx.results[i]; ok && call.MessageIndex < start {
			key := [2]int{call.MessageIndex, call.Ordinal}
			if !seen[key] {
				seen[key] = true
				contexts = append(contexts, call)
			}
		}
	}
	if anchor > 0 {
		state := idx.prefix[anchor-1]
		if state.closing >= start && state.closing < end {
			work[state.closing-start] = state.turn + state.run
		}
	}
	sort.Slice(contexts, func(i, j int) bool {
		if contexts[i].MessageIndex == contexts[j].MessageIndex {
			return contexts[i].Ordinal < contexts[j].Ordinal
		}
		return contexts[i].MessageIndex < contexts[j].MessageIndex
	})
	return work, contexts
}
