package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/termrender"
)

// statusDetails is the expanded session diagnostics /status prints, one
// labelled line per fact the footer carries in compressed form.
func (m *model) statusDetails() string {
	s := m.status
	lines := []string{termrender.Accent("Session status"), "  mode       " + m.modeText()}
	model := s.ModelRef
	if model == "" {
		model = s.Label
	}
	if model != "" {
		lines = append(lines, "  model      "+model)
	}
	if tag := contextText(s.Used, s.Window, m.compaction); tag != "" {
		lines = append(lines, "  context    "+tag)
	}
	effort := s.Effort
	if effort == "" {
		effort = "auto"
	}
	lines = append(lines, "  effort     effort "+effort)
	if body, _, ok := cacheStatus(s); ok {
		lines = append(lines, "  cache      "+body)
	}
	if rate, ok := warmCacheRate(s.SessionUsage); ok {
		lines = append(lines, "  warm cache "+fmt.Sprintf(i18n.M.ChatStatusCacheWarmFmt, fmt.Sprintf("%.2f%%", rate)))
	}
	if u := s.SessionUsage; u != nil && u.PromptTokens > 0 {
		ratio := fmt.Sprintf("%.1f%%", pct(u.CompletionTokens, u.PromptTokens))
		lines = append(lines, "  out/in     "+fmt.Sprintf(i18n.M.ChatStatusOutputRatioFmt, ratio, shortTokens(u.CompletionTokens), shortTokens(u.PromptTokens)))
	}
	if git := m.gitText(); git != "" {
		lines = append(lines, "  git        "+git)
	}
	if n := len(s.Jobs); n > 0 {
		lines = append(lines, fmt.Sprintf("  jobs       ⚙ %d", n))
	}
	if m.balance != "" {
		lines = append(lines, "  balance    "+m.balance)
	}
	if m.scr != nil && m.scr.mouseOff {
		lines = append(lines, "  mouse      "+i18n.M.MouseCaptureTag)
	}
	return strings.Join(append(lines, "  config     "+activeConfigTag()), "\n")
}

// warmCacheRate is the ledger's hit rate with its cold start taken out of both
// sides: hit over the prompt the endpoint split, the same rate the desktop's
// session overview states. Not owed until a request after the cold start split.
func warmCacheRate(u *SessionUsage) (float64, bool) {
	if u == nil || u.ColdStart == nil {
		return 0, false
	}
	c := u.ColdStart
	hit := u.CacheHit - c.CacheHit
	known := hit + (u.CacheMiss - c.CacheMiss) - (u.CacheUnknown - c.CacheUnknown)
	if known <= 0 {
		return 0, false
	}
	return pct(min(hit, known), known), true
}

// modeText is the footer's mode badge without its colour.
func (m *model) modeText() string {
	s := m.status
	switch {
	case s.ToolApprovalMode == "yolo":
		return "YOLO"
	case s.Plan:
		return "Plan"
	case s.ToolApprovalMode == "auto":
		return "Auto"
	case s.ToolApprovalMode == "readOnly":
		return "Read only"
	case s.ToolApprovalMode == "dontAsk":
		return "Don't ask"
	}
	return "Ask"
}

// contextText is the context gauge as a sentence: how full the window is and,
// where the session folds early, how far off that is.
func contextText(used, window int, c Compaction) string {
	if used == 0 || window == 0 {
		return ""
	}
	p := used * 100 / window
	threshold := 0
	switch {
	case c.Trigger > 0:
		threshold = c.Trigger * 100 / window
	case c.Ratio > 0 && c.Ratio < 1:
		threshold = int(c.Ratio * 100)
	}
	switch {
	case threshold <= 0 || threshold >= 100:
		return fmt.Sprintf("%s / %s ctx (%d%%)", shortTokens(used), shortTokens(window), p)
	case p >= threshold:
		return fmt.Sprintf("%s ctx (%d%%) · compacting soon", shortTokens(used), p)
	}
	return fmt.Sprintf("%s ctx (%d%%) · %d%% to compact", shortTokens(used), p, threshold-p)
}

// activeConfigTag names the config file in effect: a project file outranks the
// user's, so the source has to be visible.
func activeConfigTag() string {
	path := config.SourcePath()
	if path == "" {
		return "(defaults — no config file)"
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
