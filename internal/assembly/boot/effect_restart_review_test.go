package boot

import (
	"fmt"
	"strings"
	"sync/atomic"

	"reasonix/internal/contract/provider"
)

type plannedCall struct {
	name string
	args map[string]any
}

func systemText(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

var kindSeq atomic.Int64

// uniqueKind keeps provider registrations apart when the test runs repeatedly.
func uniqueKind(base string) string { return fmt.Sprintf("%s-%d", base, kindSeq.Add(1)) }
