package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// runLoopCommand handles "/loop <seconds> <prompt>", "/loop stop", or "/loop".
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
	interval, err := parseIntSeconds(args[0])
	if err != nil {
		m.notice("usage: /loop <seconds> <prompt>  (minimum 5 seconds)")
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
	m.notice(fmt.Sprintf("▸ /loop started: every %ds → %s", int(interval.Seconds()), prompt))
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
}

// showLoopStatus prints the current /loop state.
func (m *chatTUI) showLoopStatus() {
	if m.loopPrompt == "" {
		m.notice("no active loop — start one with /loop <seconds> <prompt>")
		return
	}
	preview := m.loopPrompt
	if len(preview) > 40 {
		preview = preview[:37] + "..."
	}
	m.notice(fmt.Sprintf("▸ /loop: `%s` · every %ds · iter %d", preview, int(m.loopInterval.Seconds()), m.loopIter))
}

// parseIntSeconds parses a plain integer as seconds. Minimum 5, no maximum.
func parseIntSeconds(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty interval")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid interval: %s", raw)
	}
	if n < 5 {
		return 0, fmt.Errorf("minimum interval is 5 seconds")
	}
	return time.Duration(n) * time.Second, nil
}

// pluralS returns "s" when n != 1, for simple pluralisation.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
