package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"reasonix/internal/tool"
)

type incompleteReadGrepArgs struct {
	Pattern string
	Path    string
}

type readStrategyReceiptArgs struct {
	ReadID            string   `json:"read_id"`
	SearchToolCallIDs []string `json:"search_tool_call_ids"`
	ReadToolCallIDs   []string `json:"read_tool_call_ids"`
	Conclusion        string   `json:"conclusion"`
}

func parseIncompleteReadGrepArgs(raw json.RawMessage) (incompleteReadGrepArgs, bool) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.Pattern) == "" || strings.TrimSpace(args.Path) == "" {
		return incompleteReadGrepArgs{}, false
	}
	return incompleteReadGrepArgs{Pattern: args.Pattern, Path: args.Path}, true
}

func parseReadStrategyReceiptArgs(raw json.RawMessage) (readStrategyReceiptArgs, bool) {
	var args readStrategyReceiptArgs
	if json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.ReadID) == "" {
		return readStrategyReceiptArgs{}, false
	}
	return args, true
}

func grepMatchLines(output string) []int {
	var lines []int
	seen := make(map[int]bool)
	for line := range strings.SplitSeq(output, "\n") {
		for start := 0; start < len(line); {
			colon := strings.IndexByte(line[start:], ':')
			if colon < 0 {
				break
			}
			colon += start
			next := strings.IndexByte(line[colon+1:], ':')
			if next < 0 {
				break
			}
			next += colon + 1
			n, err := strconv.Atoi(line[colon+1 : next])
			if err == nil && n > 0 {
				if !seen[n] {
					seen[n] = true
					lines = append(lines, n)
				}
				break
			}
			start = colon + 1
		}
	}
	return lines
}

func (s *incompleteReadState) resetStrategyEvidenceForVersionLocked(entry *incompleteRead, current incompleteReadFileVersion) {
	entry.searches = make(map[string]incompleteReadSearch)
	entry.reads = make(map[string]incompleteReadWindow)
	entry.targetReadID = ""
	entry.targetObserved = nil
	entry.targetEnd = 0
	entry.pendingReceipt = nil
	entry.strategyVersion = current
	entry.strategyRevision++
}

func (s *incompleteReadState) observeStrategySearch(plan *toolCallPlan, output string, visibleFull bool) incompleteReadTransition {
	if plan == nil || plan.incompleteReadRoot == "" || plan.incompleteReadAction != incompleteReadActionStrategySearch {
		return incompleteReadTransition{}
	}
	args, ok := parseIncompleteReadGrepArgs(plan.runArgs)
	if !ok {
		return incompleteReadTransition{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[plan.incompleteReadRoot]
	if entry == nil || entry.phase != incompleteReadStrategy {
		return incompleteReadTransition{}
	}
	transition := incompleteReadTransition{readID: entry.readID, path: entry.path}
	if !visibleFull || strings.Contains(output, "timed out after") {
		return transition
	}
	current := snapshotIncompleteReadFile(entry.path)
	if !sameIncompleteReadFileVersion(entry.strategyVersion, current) {
		s.resetStrategyEvidenceForVersionLocked(entry, current)
	}
	entry.searches[plan.call.ID] = incompleteReadSearch{
		callID: plan.call.ID, pattern: args.Pattern, matchLines: grepMatchLines(output),
	}
	entry.strategyRevision++
	s.roundProgress = true
	transition.strategyProgress = true
	return transition
}

func uniqueNonEmptyIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one tool call id is required")
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, fmt.Errorf("tool call ids must be non-empty")
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate tool call id %q", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

type readStrategyReceiptValidation struct {
	entry       *incompleteRead
	readID      string
	path        string
	requestPath string
	version     incompleteReadFileVersion
	revision    uint64
	readTool    tool.Tool
	searchIDs   []string
	readIDs     []string
	patterns    []string
	ranges      []string
	windows     []incompleteReadWindow
	conclusion  string
}

func cloneIncompleteReadWindow(window incompleteReadWindow) incompleteReadWindow {
	copyWindow := window
	copyWindow.observed = make([]tool.ModelTextObservation, len(window.observed))
	for i, observed := range window.observed {
		copyWindow.observed[i] = observed
		copyWindow.observed[i].LineHashes = append([]string(nil), observed.LineHashes...)
	}
	return copyWindow
}

func sameModelTextObservation(a, b tool.ModelTextObservation) bool {
	if filepath.Clean(a.Path) != filepath.Clean(b.Path) || a.StartLine != b.StartLine || len(a.LineHashes) != len(b.LineHashes) {
		return false
	}
	for i := range a.LineHashes {
		if a.LineHashes[i] != b.LineHashes[i] {
			return false
		}
	}
	return true
}

func validateReadStrategyWindows(ctx context.Context, check readStrategyReceiptValidation) error {
	observer, ok := check.readTool.(tool.ModelTextObserver)
	if check.readTool == nil || !ok {
		return fmt.Errorf("read strategy receipt: read_file cannot revalidate the selected windows")
	}
	for _, window := range check.windows {
		for _, expected := range window.observed {
			if expected.StartLine < 1 || len(expected.LineHashes) == 0 {
				return fmt.Errorf("read strategy receipt: selected read_file window has invalid host evidence")
			}
			args, _ := json.Marshal(map[string]any{
				"path": check.requestPath, "offset": expected.StartLine - 1, "limit": len(expected.LineHashes),
			})
			output, err := check.readTool.Execute(ctx, args)
			if err != nil {
				return fmt.Errorf("read strategy receipt: re-read window %d-%d: %w", expected.StartLine, expected.StartLine+len(expected.LineHashes)-1, err)
			}
			actual, ok := observer.ObserveModelText(args, output)
			if !ok || !sameModelTextObservation(actual, expected) {
				return fmt.Errorf("read strategy receipt: selected read_file window changed; repeat grep and explicit read_file windows")
			}
		}
	}
	return nil
}

func (s *incompleteReadState) rejectStrategyReceiptValidation(check readStrategyReceiptValidation, current incompleteReadFileVersion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[check.readID]
	if entry != check.entry {
		return
	}
	s.roundViolation = true
	if entry.strategyRevision == check.revision {
		s.resetStrategyEvidenceForVersionLocked(entry, current)
	}
}

func (s *incompleteReadState) submitStrategyReceipt(ctx context.Context, args readStrategyReceiptArgs) (string, error) {
	searchIDs, err := uniqueNonEmptyIDs(args.SearchToolCallIDs)
	if err != nil {
		return "", fmt.Errorf("read strategy receipt: search_tool_call_ids: %w", err)
	}
	readIDs, err := uniqueNonEmptyIDs(args.ReadToolCallIDs)
	if err != nil {
		return "", fmt.Errorf("read strategy receipt: read_tool_call_ids: %w", err)
	}
	conclusion := strings.TrimSpace(args.Conclusion)
	if conclusion == "" {
		return "", fmt.Errorf("read strategy receipt: conclusion is required")
	}

	s.mu.Lock()
	entry := s.entries[strings.TrimSpace(args.ReadID)]
	if entry == nil || entry.phase != incompleteReadStrategy {
		s.roundViolation = true
		s.mu.Unlock()
		return "", fmt.Errorf("read strategy receipt: active read_id %q was not found or still has an unfinished page", args.ReadID)
	}

	patterns := make([]string, 0, len(searchIDs))
	var matchedLines []int
	for _, id := range searchIDs {
		search, ok := entry.searches[id]
		if !ok {
			s.roundViolation = true
			s.mu.Unlock()
			return "", fmt.Errorf("read strategy receipt: grep tool call %q is not complete evidence for read_id %q", id, entry.readID)
		}
		patterns = append(patterns, search.pattern)
		matchedLines = append(matchedLines, search.matchLines...)
	}

	ranges := make([]string, 0, len(readIDs))
	windows := make([]incompleteReadWindow, 0, len(readIDs))
	overlaps := len(matchedLines) == 0
	for _, id := range readIDs {
		window, ok := entry.reads[id]
		if !ok || len(window.observed) == 0 {
			s.roundViolation = true
			s.mu.Unlock()
			return "", fmt.Errorf("read strategy receipt: read_file tool call %q is not a fully consumed explicit window for read_id %q", id, entry.readID)
		}
		ranges = append(ranges, fmt.Sprintf("%d-%d", window.startLine, window.endLine))
		windows = append(windows, cloneIncompleteReadWindow(window))
		for _, line := range matchedLines {
			if line >= window.startLine && line <= window.endLine {
				overlaps = true
				break
			}
		}
	}
	if !overlaps {
		s.roundViolation = true
		s.mu.Unlock()
		return "", fmt.Errorf("read strategy receipt: no selected read_file window overlaps a cited grep match line")
	}
	check := readStrategyReceiptValidation{
		entry: entry, readID: entry.readID, path: entry.path, requestPath: entry.requestPath,
		version: entry.strategyVersion, revision: entry.strategyRevision, readTool: entry.readTool,
		searchIDs: searchIDs, readIDs: readIDs, patterns: patterns, ranges: ranges,
		windows: windows, conclusion: conclusion,
	}
	s.mu.Unlock()

	current := snapshotIncompleteReadFile(check.path)
	if !sameIncompleteReadFileVersion(check.version, current) {
		s.rejectStrategyReceiptValidation(check, current)
		return "", fmt.Errorf("read strategy receipt: the target file changed; repeat grep and explicit read_file windows")
	}
	if err := validateReadStrategyWindows(ctx, check); err != nil {
		s.rejectStrategyReceiptValidation(check, snapshotIncompleteReadFile(check.path))
		return "", err
	}
	current = snapshotIncompleteReadFile(check.path)
	if !sameIncompleteReadFileVersion(check.version, current) {
		s.rejectStrategyReceiptValidation(check, current)
		return "", fmt.Errorf("read strategy receipt: the target file changed during validation; repeat grep and explicit read_file windows")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entry = s.entries[check.readID]
	if entry != check.entry || entry.phase != incompleteReadStrategy || entry.strategyRevision != check.revision || entry.pendingReceipt != nil {
		s.roundViolation = true
		return "", fmt.Errorf("read strategy receipt: strategy evidence changed during validation; submit a new receipt")
	}

	entry.pendingReceipt = &incompleteReadReceipt{
		searchIDs: check.searchIDs, readIDs: check.readIDs, conclusion: check.conclusion,
	}
	entry.strategyRevision++
	s.roundProgress = true
	payload, _ := json.Marshal(map[string]any{
		"read_id":         entry.readID,
		"path":            entry.path,
		"search_patterns": check.patterns,
		"read_ranges":     check.ranges,
		"conclusion":      check.conclusion,
		"status":          "validated_pending_round_boundary",
		"whole_file_read": false,
	})
	return string(payload), nil
}
