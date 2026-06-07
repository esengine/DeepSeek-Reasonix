package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// runLoopCommand handles "/loop <interval> <prompt>", "/loop stop", or "/loop".
func (m *chatTUI) runLoopCommand(input string) {
	args := strings.Fields(strings.TrimSpace(strings.TrimPrefix(input, "/loop")))
	if len(args) == 0 {
		m.showLoopStatus()
		return
	}
	first := strings.ToLower(args[0])
	if first == "stop" || first == "off" || first == "cancel" {
		m.stopLoop()
		return
	}
	interval, err := parseLoopInterval(args[0])
	if err != nil {
		m.notice("usage: /loop <interval> <prompt>  (interval = 5s..6h, e.g. 30s, 5m, 1h)")
		return
	}
	prompt := strings.Join(args[1:], " ")
	if prompt == "" {
		m.notice("usage: /loop " + args[0] + " <prompt>")
		return
	}
	m.loopPrompt = prompt
	m.loopInterval = interval
	m.loopIter = 0
	m.notice(fmt.Sprintf("▸ /loop started: every %s → %s", formatDuration(interval), prompt))
	// Fire the first iteration immediately
	m.loopIter++
	m.startTurnWithRaw(prompt, "/loop: "+prompt, prompt, prompt)
}

// stopLoop cancels an active /loop.
func (m *chatTUI) stopLoop() {
	if m.loopPrompt == "" {
		m.notice("no active loop")
		return
	}
	m.notice(fmt.Sprintf("▸ /loop stopped (after %d iter%s)", m.loopIter, pluralS(m.loopIter)))
	m.loopPrompt = ""
	m.loopInterval = 0
	m.loopIter = 0
	m.loopTickStart = time.Time{}
}

// showLoopStatus prints the current /loop state.
func (m *chatTUI) showLoopStatus() {
	if m.loopPrompt == "" {
		m.notice("no active loop — start one with /loop <interval> <prompt>")
		return
	}
	preview := m.loopPrompt
	if len(preview) > 40 {
		preview = preview[:37] + "..."
	}
	m.notice(fmt.Sprintf("▸ /loop: `%s` · every %s · iter %d", preview, formatDuration(m.loopInterval), m.loopIter))
}

// parseLoopInterval parses a duration string like "30s", "5m", "1h".
func parseLoopInterval(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty interval")
	}
	// Handle unit suffixes: s, m, h
	last := s[len(s)-1]
	var numStr string
	var multiplier time.Duration
	switch last {
	case 's':
		numStr = s[:len(s)-1]
		multiplier = time.Second
	case 'm':
		numStr = s[:len(s)-1]
		multiplier = time.Minute
	case 'h':
		numStr = s[:len(s)-1]
		multiplier = time.Hour
	default:
		// No suffix — assume seconds
		numStr = s
		multiplier = time.Second
	}
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid interval: %s", raw)
	}
	d := time.Duration(n * float64(multiplier))
	// Clamp: 5 seconds to 6 hours
	const minInterval = 5 * time.Second
	const maxInterval = 6 * time.Hour
	if d < minInterval {
		d = minInterval
	}
	if d > maxInterval {
		d = maxInterval
	}
	return d, nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		s := int(d.Seconds())
		return fmt.Sprintf("%ds", s)
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
