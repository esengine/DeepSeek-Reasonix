package agent

import (
	"fmt"
	"time"
)

type rootWallBudget struct {
	started time.Time
	limit   time.Duration
}

func (b rootWallBudget) exceeded() (string, string) {
	if b.limit > 0 && !b.started.IsZero() {
		if elapsed := time.Since(b.started); elapsed >= b.limit {
			return "time", fmt.Sprintf("root task ran %s, past the %s budget across delegated runs", elapsed.Round(time.Second), b.limit)
		}
	}
	return "", ""
}
