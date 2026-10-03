package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/frontend/termrender"
)

const footerIndent = "  "

func footerLabel(s string) string { return termrender.ThemeFg(termrender.ActiveTheme().Subtle, s) }

func footerValue(s string) string { return termrender.ThemeFg(termrender.ActiveTheme().Muted, s) }

func footerMetric(label, value string) string {
	if value == "" {
		return ""
	}
	return footerLabel(label) + " " + value
}

// statusBlock is the footer under the composer: interaction state with the
// model on the right, a quiet rule, then the session's telemetry.
func (m *model) statusBlock() []string {
	width := max(m.width-1, 1)
	left := m.modeTag()
	if vi := m.viModeTag(); vi != "" {
		left += " · " + vi
	}
	left += " · " + m.stateText()
	first := layoutSides(footerIndent+left, m.modelGroup(), width)
	rows := strings.Split(first, "\n")
	groups := m.telemetry()
	if m.statusline != "" {
		groups = []string{m.statusline}
	}
	data := m.dataRows(groups, width)
	if len(data) > 0 {
		rows = append(rows, footerIndent+termrender.ThemeFg(termrender.ActiveTheme().Border, strings.Repeat("─", max(width-len(footerIndent), 1))))
		rows = append(rows, data...)
	}
	return rows
}

// dataRows puts the work tree's identity at the left of the row the telemetry
// sits on, falling back to a row of its own when the two do not fit together.
func (m *model) dataRows(groups []string, width int) []string {
	git := m.gitText()
	if git == "" {
		return packGroups(groups, width)
	}
	line := termrender.Truncate(footerIndent+git, width, "…")
	tel := strings.Join(nonEmpty(groups), "  ")
	if tel == "" {
		return []string{line}
	}
	if lw, tw := termrender.VisibleWidth(line), termrender.VisibleWidth(tel); lw+2+tw <= width {
		return []string{line + strings.Repeat(" ", width-lw-tw) + tel}
	}
	return append([]string{line}, packGroups(groups, width)...)
}

func nonEmpty(groups []string) []string {
	var out []string
	for _, g := range groups {
		if g != "" {
			out = append(out, g)
		}
	}
	return out
}

// gitText is workspace@branch followed by what differs from HEAD, as 1.x
// draws it: counts that are zero stay out.
func (m *model) gitText() string {
	g := m.git
	if !g.Repo || strings.TrimSpace(g.Name) == "" || strings.TrimSpace(g.Branch) == "" {
		return ""
	}
	t := termrender.ActiveTheme()
	branch := footerValue(g.Branch)
	if g.Detached {
		branch = termrender.Yellow(g.Branch)
	}
	out := termrender.ThemeFg(t.Warn, g.Name) + termrender.Dim("@") + branch
	var dirt []string
	if g.Added > 0 || g.Removed > 0 {
		dirt = append(dirt, termrender.Green(fmt.Sprintf("+%d", g.Added)), termrender.Red(fmt.Sprintf("-%d", g.Removed)))
	}
	if g.Untracked > 0 {
		dirt = append(dirt, termrender.Yellow(fmt.Sprintf("?%d", g.Untracked)))
	}
	if len(dirt) > 0 {
		out += "  " + strings.Join(dirt, " ")
	}
	return out
}

// statuslinePayload is the context a [statusline] command reads on stdin.
func statuslinePayload(s Status) string {
	label := s.Label
	if label == "" {
		label = modelName(s.ModelRef)
	}
	cwd := s.Cwd
	if cwd == "" {
		cwd = s.WorkspaceRoot
	}
	b, _ := json.Marshal(map[string]any{
		"model":         label,
		"contextUsed":   s.Used,
		"contextWindow": s.Window,
		"cwd":           cwd,
	})
	return string(b)
}

func (m *model) modeTag() string {
	s := m.status
	switch {
	case m.shell:
		return termrender.Badge(tagShellColor, tagLight, "Shell")
	case s.ToolApprovalMode == "yolo":
		return termrender.Badge(tagYoloColor, tagLight, "YOLO")
	case s.Plan:
		return termrender.Badge(tagPlanColor, tagLight, "Plan")
	case s.ToolApprovalMode == "auto":
		return termrender.Badge(tagAskColor, tagDark, "Auto")
	case s.ToolApprovalMode == "readOnly":
		return termrender.Badge(tagAskColor, tagDark, "Read only")
	case s.ToolApprovalMode == "dontAsk":
		return termrender.Badge(tagAskColor, tagDark, "Don't ask")
	}
	return termrender.Badge(tagAskColor, tagDark, "Ask")
}

func (m *model) stateText() string {
	open := m.tr.OpenPrompt()
	tag := ""
	if m.scr != nil && m.scr.mouseOff {
		tag = " · " + termrender.Dim(i18n.M.MouseCaptureTag)
	}
	switch {
	case m.setup != nil:
		return footerValue(i18n.M.SetupTitle)
	case m.catalog != nil:
		return footerValue(m.catalog.kind + " picker")
	case m.flashText() != "":
		return termrender.Green(m.flashText()) + tag
	case open != nil && open.Kind == ItemAsk:
		return footerLabel(m.viHint(i18n.M.ChatStatusQuestion, i18n.M.ChatStatusQuestionVi))
	case open != nil && open.Approval.Kind == "plan":
		return footerLabel(m.viHint(i18n.M.ChatStatusPlanApproval, i18n.M.ChatStatusPlanApprovalVi))
	case open != nil:
		return footerLabel(m.viHint(i18n.M.ChatStatusToolApproval, i18n.M.ChatStatusToolApprovalVi))
	case !m.quitArmedAt.IsZero() && time.Since(m.quitArmedAt) < quitArmWindow:
		return i18n.M.CtrlCQuitHint
	case m.shell:
		return i18n.M.ShellModeHint
	case m.tr.Running:
		return footerLabel(i18n.M.ChatStatusCycleHintCompact)
	}
	return footerValue(i18n.M.ChatStatusIdle) + " · " + footerLabel(i18n.M.ChatStatusCycleHintCompact) + tag
}

func (m *model) modelGroup() string {
	s := m.status
	label := s.Label
	if label == "" {
		label = modelName(s.ModelRef)
	}
	var fields []string
	if label != "" {
		fields = append(fields, footerMetric(i18n.M.ChatStatusModelLabel, termrender.ThemeFg(termrender.ActiveTheme().Info, label)))
	}
	switch s.Effort {
	case "", "auto":
		fields = append(fields, footerMetric(i18n.M.ChatStatusEffortLabel, footerValue("auto")))
	default:
		fields = append(fields, footerMetric(i18n.M.ChatStatusEffortLabel, termrender.ThemeFg(termrender.ActiveTheme().Info, termrender.Bold(s.Effort))))
	}
	return strings.Join(fields, "   ")
}

func (m *model) telemetry() []string {
	s := m.status
	var out []string
	if body, rate, ok := cacheStatus(s); ok {
		out = append(out, footerMetric(i18n.M.ChatStatusCacheLabel, termrender.ThemeFg(cacheColor(rate), body)))
	}
	out = append(out, contextGroups(s.Used, s.Window, m.compaction)...)
	if m.balance != "" {
		out = append(out, footerMetric(i18n.M.ChatStatusBalanceLabel, footerValue(m.balance)))
	}
	if cost := quoteText(s.SessionCostQuote); cost != "" {
		out = append(out, footerMetric(i18n.M.ChatStatusCostLabel, footerValue(cost)))
	}
	return out
}

func cacheStatus(s Status) (string, float64, bool) {
	var parts []string
	rate := 0.0
	if u := s.LastUsage; u != nil && u.CacheHitTokens+u.CacheMissTokens > 0 {
		rate = pct(u.CacheHitTokens, u.CacheHitTokens+u.CacheMissTokens)
		parts = append(parts, fmt.Sprintf(i18n.M.ChatStatusCacheNowFmt, fmt.Sprintf("%.2f%%", rate)))
	}
	if total := s.CacheHit + s.CacheMiss; total > 0 {
		rate = pct(s.CacheHit, total)
		parts = append(parts, fmt.Sprintf(i18n.M.ChatStatusCacheAvgFmt, fmt.Sprintf("%.2f%%", rate)))
	}
	return strings.Join(parts, " · "), rate, len(parts) > 0
}

func pct(n, of int) float64 { return float64(n) * 100 / float64(of) }

func cacheColor(rate float64) termrender.Color {
	t := termrender.ActiveTheme()
	switch {
	case rate >= 80:
		return t.Success
	case rate >= 50:
		return t.Info
	}
	return t.Warn
}

// contextGroups shows how full the window is and, where the session folds
// before the window ends, how much room is left before it does.
func contextGroups(used, window int, c Compaction) []string {
	if used == 0 || window == 0 {
		return nil
	}
	t := termrender.ActiveTheme()
	p := used * 100 / window
	threshold := 0
	switch {
	case c.Trigger > 0:
		threshold = c.Trigger * 100 / window
	case c.Ratio > 0 && c.Ratio < 1:
		threshold = int(c.Ratio * 100)
	}
	if threshold <= 0 || threshold >= 100 {
		color := t.Muted
		switch {
		case p >= 85:
			color = t.Danger
		case p >= 60:
			color = t.Warn
		}
		return []string{footerMetric(i18n.M.ChatStatusContextLabel, termrender.ThemeFg(color, fmt.Sprintf("%s / %s (%d%%)", shortTokens(used), shortTokens(window), p)))}
	}
	left := max(threshold-p, 0)
	ctxColor, foldColor := t.Muted, t.Muted
	switch {
	case p >= threshold:
		ctxColor, foldColor = t.Warn, t.Danger
	case left <= 10:
		ctxColor, foldColor = t.Warn, t.Warn
	}
	return []string{
		footerMetric(i18n.M.ChatStatusContextLabel, termrender.ThemeFg(ctxColor, fmt.Sprintf("%s (%d%%)", shortTokens(used), p))),
		footerMetric(i18n.M.ChatStatusCompactLabel, termrender.ThemeFg(foldColor, fmt.Sprintf("%d%%", left))),
	}
}

// quoteText is the session's spend, or "" where the kernel could not price
// it: an estimate never shows as a bare zero.
func quoteText(q *CostQuote) string {
	if q == nil || !q.CostComplete {
		return ""
	}
	money := q.Original
	if q.Selected != nil {
		money = *q.Selected
	}
	amount, err := strconv.ParseFloat(money.Amount, 64)
	if err != nil || amount <= 0 {
		return ""
	}
	return fmt.Sprintf("≈%s%.4f", pricing.CurrencySymbol(money.Currency), amount)
}

// layoutSides puts right against the right edge of left's row, or on a row of
// its own when the two do not fit side by side.
func layoutSides(left, right string, width int) string {
	if right == "" {
		return left
	}
	lw, rw := termrender.VisibleWidth(left), termrender.VisibleWidth(right)
	if lw+2+rw <= width {
		return left + strings.Repeat(" ", width-lw-rw) + right
	}
	return left + "\n" + footerIndent + right
}

// packGroups lays groups left to right, starting a new row only between them.
func packGroups(groups []string, width int) []string {
	var rows []string
	cur := ""
	for _, g := range groups {
		if g == "" {
			continue
		}
		next := footerIndent + g
		if cur != "" {
			next = cur + "  " + g
		}
		if cur != "" && termrender.VisibleWidth(next) > width {
			rows = append(rows, cur)
			next = footerIndent + g
		}
		cur = next
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	return rows
}
