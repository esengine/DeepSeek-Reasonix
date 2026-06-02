package control

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
)

// DefaultAutoMemoryIdle is the conservative delay used when auto_memory is on
// but auto_memory_idle is omitted.
const DefaultAutoMemoryIdle = 10 * time.Minute

func normalizeAutoMemory(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), "on")
}

// ParseAutoMemoryIdle parses the config duration for auto-memory. An empty value
// uses DefaultAutoMemoryIdle.
func ParseAutoMemoryIdle(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return DefaultAutoMemoryIdle, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be greater than zero")
	}
	return d, nil
}

func (c *Controller) cancelAutoMemoryTimerLocked() {
	if c.autoMemoryTimer != nil {
		c.autoMemoryTimer.Stop()
		c.autoMemoryTimer = nil
	}
}

func (c *Controller) scheduleAutoMemory() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.autoMemoryReadyLocked() {
		return
	}
	c.cancelAutoMemoryTimerLocked()
	c.autoMemoryTimer = time.AfterFunc(c.autoMemoryIdle, func() {
		if err := c.FlushAutoMemory(context.Background()); err != nil {
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "auto memory skipped: " + err.Error()})
		}
	})
}

func (c *Controller) autoMemoryReadyLocked() bool {
	return c.autoMemory &&
		c.autoMemoryIdle > 0 &&
		c.executor != nil &&
		c.mem != nil &&
		c.mem.Store.Dir != ""
}

// FlushAutoMemory refreshes the day's long-term memory summary for conversation
// messages that have not yet been folded into it. It is safe to call when
// auto-memory is disabled; in that case it is a no-op.
func (c *Controller) FlushAutoMemory(ctx context.Context) error {
	c.mu.Lock()
	if !c.autoMemoryReadyLocked() || c.running || c.autoMemoryFlushing {
		c.mu.Unlock()
		return nil
	}
	c.cancelAutoMemoryTimerLocked()
	exec := c.executor
	mem := c.mem
	cursor := c.autoMemoryCursor
	now := c.autoMemoryNow
	c.autoMemoryFlushing = true
	c.mu.Unlock()

	capturedEnd := 0
	var savedPath string
	defer func() {
		c.mu.Lock()
		c.autoMemoryFlushing = false
		c.mu.Unlock()
	}()

	msgs := exec.Session().Snapshot()
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(msgs) {
		c.mu.Lock()
		c.autoMemoryCursor = len(msgs)
		c.mu.Unlock()
		return nil
	}
	capturedEnd = len(msgs)
	region := msgs[cursor:]
	if !hasMemorySummaryContent(region) {
		c.mu.Lock()
		if c.autoMemoryCursor < capturedEnd {
			c.autoMemoryCursor = capturedEnd
		}
		c.mu.Unlock()
		return nil
	}

	day := now().Format("2006-01-02")
	name := "daily-summary-" + day
	existing := existingMemoryBody(mem.Store, name)
	summary, err := exec.SummarizeDailyMemory(ctx, day, existing, region)
	if err != nil {
		return err
	}
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	savedPath, err = mem.Store.Save(memory.Memory{
		Name:        name,
		Title:       "Daily summary " + day,
		Description: "Daily auto memory summary for " + day,
		Type:        memory.TypeProject,
		Body:        summary,
	})
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.autoMemoryCursor < capturedEnd {
		c.autoMemoryCursor = capturedEnd
	}
	c.pendingMemory = append(c.pendingMemory, "Daily auto memory summary for "+day+" was refreshed at "+savedPath+".")
	c.refreshMemoryLocked()
	c.mu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "auto memory: refreshed daily summary"})
	return nil
}

func hasMemorySummaryContent(msgs []provider.Message) bool {
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			continue
		}
		if strings.TrimSpace(m.Content) != "" || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

func existingMemoryBody(store memory.Store, name string) string {
	for _, m := range store.List() {
		if m.Name == name {
			return m.Body
		}
	}
	return ""
}
