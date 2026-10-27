package sessionstore

import "reasonix/internal/contract/provider"

// ReadMessageWindow captures a bounded forward range and rewrite identity under one lock.
func (s *Session) ReadMessageWindow(after, limit int) HistoryPageRead {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := len(s.Messages)
	limit = min(max(limit, 1), 2000)
	if after < 0 || after > count {
		after = max(0, count-limit)
	}
	end := min(count, after+limit)
	idx := s.historyIndexLocked()
	work, calls := idx.pageContext(after, end, count)
	return HistoryPageRead{Identity: idx.identity, Version: s.version, Start: after, End: end, Total: count,
		Messages: append([]provider.Message{}, s.Messages[after:end]...), WorkMs: work, ToolContext: calls}
}
