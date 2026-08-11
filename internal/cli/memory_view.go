package cli

import (
	"time"

	"reasonix/internal/control"
	"reasonix/internal/memory"
)

func renderMemory(width int, set *memory.Set) string {
	return viewProtectLines(control.RenderMemorySummary(set, time.Now().UTC()), width)
}
