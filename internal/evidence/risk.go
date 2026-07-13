package evidence

import (
	"path/filepath"
	"strings"
)

// RiskLevel classifies the latest post-mutation change set for adaptive review.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// highRiskPathHints elevate ordinary production edits to High when the path
// touches auth, crypto, networking, providers, plugins, sandbox, config,
// migrations, persistence, or concurrency.
var highRiskPathHints = []string{
	"auth", "permission", "secret", "credential", "token", "password", "oauth",
	"crypto", "encrypt", "decrypt", "tls", "ssl", "keyring",
	"network", "proxy", "http", "websocket", "provider",
	"plugin", "mcp", "tool", "schema", "sandbox",
	"config", "migrate", "migration", "persist", "store", "database", "db",
	"concurrent", "mutex", "race", "lock", "atomic",
}

// highRiskToolHints elevate opaque or privileged mutation surfaces.
var highRiskToolHints = []string{
	"mcp__", "install_source", "install_skill", "plugin",
}

// ClassifyMutationRisk scores the change set after the latest mutation.
// Low: docs/tests/i18n/pure presentation only, with no opaque writes.
// Medium: ordinary production code or limited multi-file edits.
// High: security-sensitive surfaces, opaque mutations, or 10+ paths.
func ClassifyMutationRisk(receipts []Receipt, after int) RiskLevel {
	start := after + 1
	if start < 0 {
		start = 0
	}
	var paths []string
	seen := map[string]bool{}
	opaque := false
	hasProd := false
	onlyLow := true

	// Include the mutation receipt itself.
	if after >= 0 && after < len(receipts) {
		r := receipts[after]
		if r.Success && r.Mutation {
			if len(r.Paths) == 0 {
				opaque = true
			}
			for _, p := range r.Paths {
				if !seen[p] {
					seen[p] = true
					paths = append(paths, p)
				}
			}
			if toolLooksHighRisk(r.ToolName) {
				return RiskHigh
			}
		}
	}
	for i := start; i < len(receipts); i++ {
		r := receipts[i]
		if !r.Success || !r.Mutation {
			continue
		}
		if len(r.Paths) == 0 {
			opaque = true
		}
		if toolLooksHighRisk(r.ToolName) {
			return RiskHigh
		}
		for _, p := range r.Paths {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	if opaque {
		return RiskHigh
	}
	if len(paths) == 0 {
		return RiskLow
	}
	if len(paths) >= 10 {
		return RiskHigh
	}
	for _, p := range paths {
		if pathLooksHighRisk(p) {
			return RiskHigh
		}
		if !pathLooksLowRisk(p) {
			onlyLow = false
			hasProd = true
		}
	}
	if onlyLow && !hasProd {
		return RiskLow
	}
	return RiskMedium
}

// MutationRiskAfter classifies risk from the ledger using the latest mutation.
func (l *Ledger) MutationRiskAfter(after int) RiskLevel {
	if l == nil {
		return RiskLow
	}
	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	return ClassifyMutationRisk(receipts, after)
}

// PathsSince returns distinct paths from successful mutation/write receipts at
// or after the given index (inclusive of the mutation itself when after >= 0).
func (l *Ledger) PathsSince(after int) []string {
	if l == nil {
		return nil
	}
	start := after
	if start < 0 {
		start = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for i := start; i < len(l.receipts); i++ {
		r := l.receipts[i]
		if !r.Success || (!r.Mutation && !r.Write) {
			continue
		}
		for _, p := range r.Paths {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func pathLooksHighRisk(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))
	for _, hint := range highRiskPathHints {
		if strings.Contains(lower, hint) || strings.Contains(base, hint) {
			return true
		}
	}
	return false
}

func pathLooksLowRisk(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(lower)
	if strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, "_test.ts") ||
		strings.HasSuffix(lower, ".test.ts") || strings.HasSuffix(lower, ".test.tsx") ||
		strings.HasSuffix(lower, "_spec.ts") || strings.HasSuffix(lower, ".spec.ts") {
		return true
	}
	if strings.Contains(lower, "/testdata/") || strings.Contains(lower, "/__tests__/") ||
		strings.Contains(lower, "/fixtures/") {
		return true
	}
	switch {
	case strings.HasSuffix(base, ".md"), strings.HasSuffix(base, ".mdx"),
		strings.HasSuffix(base, ".txt"), strings.HasSuffix(base, ".rst"):
		return true
	case strings.Contains(lower, "/docs/"), strings.Contains(lower, "/locales/"),
		strings.Contains(lower, "/i18n/"), strings.HasPrefix(base, "readme"):
		return true
	case strings.HasSuffix(base, ".css") && !strings.Contains(lower, "sandbox"):
		// Pure presentation styles are low risk unless mixed with other paths.
		return true
	}
	return false
}

func toolLooksHighRisk(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, hint := range highRiskToolHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}
