package delegation

import (
	"reasonix/internal/runtime/agent"
	"strings"
)

// splitHostReceipts separates a child's own prose from the host attestation
// appended to it. Aggregates truncate prose to fit a budget; the attestation is
// bounded already and must never be the part that gets cut.
func splitHostReceipts(answer string) (prose, receipts string) {
	idx := strings.LastIndex(answer, agent.HostReceiptsHeader)
	if idx < 0 {
		return answer, ""
	}
	return strings.TrimRight(answer[:idx], "\n"), strings.TrimSpace(answer[idx:])
}

// boundedHostReceipts trims an attestation to fit limit bytes. The header and
// any violation line always survive: a parent may lose the detail of what
// changed, but never the fact that a write left the declared claim.
func boundedHostReceipts(receipts string, limit int) string {
	if receipts == "" || len(receipts) <= limit {
		return receipts
	}
	lines := strings.Split(receipts, "\n")
	var violations []string
	for _, line := range lines[1:] {
		if strings.Contains(line, agent.HostReceiptsViolationLabel) {
			violations = append(violations, line)
		}
	}
	if len(violations) == 0 {
		return utf8Prefix(lines[0], limit)
	}
	// A claim escape outranks the header it would normally sit under.
	if withHeader := strings.Join(append(lines[:1:1], violations...), "\n"); len(withHeader) <= limit {
		return withHeader
	}
	return utf8Prefix(strings.Join(violations, "\n"), limit)
}
