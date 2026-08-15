package evidence

import (
	"encoding/json"
	pathpkg "path"
	"slices"
	"strings"
)

// RiskLevel classifies a post-mutation change set for adaptive review.
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
	return ClassifyMutationRiskWithin(receipts, after, "")
}

// ClassifyMutationRiskWithin scores mutations after first normalizing absolute
// paths against workspaceRoot. Callers that own a workspace should supply it:
// checkout/temp ancestors are not change scope, while every path component
// inside the workspace remains available to the sensitive-surface classifier.
func ClassifyMutationRiskWithin(receipts []Receipt, after int, workspaceRoot string) RiskLevel {
	start := max(after+1, 0)
	var paths []string
	seen := map[string]bool{}
	opaque := false
	hasProd := false
	onlyLow := true

	// Include the mutation receipt itself.
	if after >= 0 && after < len(receipts) {
		r := receipts[after]
		if r.Success && r.Mutation {
			if len(r.Paths) == 0 && !memoryOnlyMutation(r.ToolName) {
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
		if len(r.Paths) == 0 && !memoryOnlyMutation(r.ToolName) {
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
		if pathLooksHighRisk(p, workspaceRoot) {
			return RiskHigh
		}
		if !pathLooksLowRisk(p, workspaceRoot) {
			onlyLow = false
			hasProd = true
		}
	}
	if onlyLow && !hasProd {
		return RiskLow
	}
	return RiskMedium
}

// ClassifyToolCallMutationRisk projects the risk of a concrete tool call
// before it is allowed to mutate state. It uses the same receipt/path rules as
// post-mutation classification, but the projected receipt is never recorded:
// callers can ratchet host policy before permission or execution without an
// auxiliary model request or a false success in the evidence ledger.
func ClassifyToolCallMutationRisk(toolName string, args json.RawMessage, readOnly bool) RiskLevel {
	return ClassifyToolCallMutationRiskWithin("", toolName, args, readOnly)
}

// ClassifyToolCallMutationRiskWithin projects one concrete mutation after
// normalizing its declared paths against workspaceRoot.
func ClassifyToolCallMutationRiskWithin(workspaceRoot, toolName string, args json.RawMessage, readOnly bool) RiskLevel {
	if readOnly {
		return RiskLow
	}
	receipt := ReceiptFromToolCall(toolName, args, true, readOnly)
	if !receipt.Mutation {
		return RiskLow
	}
	return ClassifyMutationRiskWithin([]Receipt{receipt}, 0, workspaceRoot)
}

// MutationRiskAfter classifies risk from the ledger starting at one mutation.
func (l *Ledger) MutationRiskAfter(after int) RiskLevel {
	return l.MutationRiskAfterWithin(after, "")
}

// MutationRiskAfterWithin classifies ledger mutations after normalizing paths
// against workspaceRoot.
func (l *Ledger) MutationRiskAfterWithin(after int, workspaceRoot string) RiskLevel {
	if l == nil {
		return RiskLow
	}
	l.mu.Lock()
	receipts := append([]Receipt(nil), l.receipts...)
	l.mu.Unlock()
	return ClassifyMutationRiskWithin(receipts, after, workspaceRoot)
}

// MutationRisk classifies all successful mutations in the current ledger. Risk
// ratchets must use the complete turn scope: a later low-risk edit must never
// hide an earlier security-sensitive or opaque mutation.
func (l *Ledger) MutationRisk() RiskLevel {
	return l.MutationRiskAfter(-1)
}

// MutationRiskWithin classifies the complete turn after normalizing paths
// against workspaceRoot.
func (l *Ledger) MutationRiskWithin(workspaceRoot string) RiskLevel {
	return l.MutationRiskAfterWithin(-1, workspaceRoot)
}

// PathsSince returns distinct paths from successful mutation/write receipts at
// or after the given index (inclusive of the mutation itself when after >= 0).
func (l *Ledger) PathsSince(after int) []string {
	if l == nil {
		return nil
	}
	start := max(after, 0)
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

func pathLooksHighRisk(path, workspaceRoot string) bool {
	relevant := riskRelevantPath(path, workspaceRoot)
	lower := strings.ToLower(relevant)
	base := strings.ToLower(pathpkg.Base(relevant))
	for _, hint := range highRiskPathHints {
		if strings.Contains(lower, hint) || strings.Contains(base, hint) {
			return true
		}
	}
	return false
}

// riskRelevantPath keeps relative paths intact and makes workspace-owned
// absolute paths relative without discarding deep owner directories. When no
// workspace root is available, only Go's recognizable t.TempDir prefix is
// removed; every other absolute path stays conservative and fully visible.
func riskRelevantPath(path, workspaceRoot string) string {
	normalized := normalizeRiskPath(path)
	if !riskPathIsAbs(normalized) {
		return normalized
	}
	if relative, ok := riskPathWithin(workspaceRoot, normalized); ok {
		return relative
	}
	parts := strings.FieldsFunc(normalized, func(r rune) bool { return r == '/' })
	for i := 0; i+2 < len(parts); i++ {
		if goTestTempComponent(parts[i]) && allDecimal(parts[i+1]) {
			// A real absolute owner directory named provider/auth/etc. must not
			// disappear merely because a descendant happens to resemble the
			// directory shape produced by testing.T.TempDir.
			if pathPartsLookHighRisk(parts[:i]) {
				return normalized
			}
			return strings.Join(parts[i+2:], "/")
		}
	}
	return normalized
}

func normalizeRiskPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" {
		return ""
	}
	cleaned := pathpkg.Clean(value)
	// path.Clean treats a Windows drive prefix as an ordinary path component
	// and removes the root slash from "C:/". Restore it so drive roots remain
	// absolute and can safely relativize paths in cross-platform evidence.
	if len(value) == 3 && value[1] == ':' && value[2] == '/' && len(cleaned) == 2 && cleaned[1] == ':' {
		return cleaned + "/"
	}
	return cleaned
}

func riskPathIsAbs(value string) bool {
	return strings.HasPrefix(value, "/") || (len(value) >= 3 && value[1] == ':' && value[2] == '/')
}

func riskPathWithin(workspaceRoot, value string) (string, bool) {
	root := normalizeRiskPath(workspaceRoot)
	value = normalizeRiskPath(value)
	if root == "" || !riskPathIsAbs(root) || !riskPathIsAbs(value) {
		return "", false
	}
	rootCompare, valueCompare := root, value
	if len(root) >= 3 && root[1] == ':' {
		rootCompare = strings.ToLower(root)
		valueCompare = strings.ToLower(value)
	}
	if valueCompare == rootCompare {
		return ".", true
	}
	prefix := rootCompare + "/"
	relativeStart := len(root) + 1
	if strings.HasSuffix(rootCompare, "/") {
		prefix = rootCompare
		relativeStart = len(root)
	}
	if !strings.HasPrefix(valueCompare, prefix) {
		return "", false
	}
	return value[relativeStart:], true
}

func pathPartsLookHighRisk(parts []string) bool {
	lower := strings.ToLower(strings.Join(parts, "/"))
	for _, hint := range highRiskPathHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func goTestTempComponent(value string) bool {
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "test") {
		return false
	}
	i := len(lower)
	for i > len("test") && lower[i-1] >= '0' && lower[i-1] <= '9' {
		i--
	}
	return i < len(lower)
}

func allDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func pathLooksLowRisk(path, workspaceRoot string) bool {
	relevant := riskRelevantPath(path, workspaceRoot)
	lower := strings.ToLower(relevant)
	base := pathpkg.Base(lower)
	if strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, "_test.ts") ||
		strings.HasSuffix(lower, ".test.ts") || strings.HasSuffix(lower, ".test.tsx") ||
		strings.HasSuffix(lower, "_spec.ts") || strings.HasSuffix(lower, ".spec.ts") {
		return true
	}
	if !riskPathIsAbs(relevant) && (hasRiskPathSegment(lower, "testdata") ||
		hasRiskPathSegment(lower, "__tests__") || hasRiskPathSegment(lower, "fixtures")) {
		return true
	}
	switch {
	case strings.HasSuffix(base, ".md"), strings.HasSuffix(base, ".mdx"),
		strings.HasSuffix(base, ".txt"), strings.HasSuffix(base, ".rst"):
		return true
	case !riskPathIsAbs(relevant) && (hasRiskPathSegment(lower, "docs") ||
		hasRiskPathSegment(lower, "locales") || hasRiskPathSegment(lower, "i18n")),
		strings.HasPrefix(base, "readme"):
		return true
	case strings.HasSuffix(base, ".css") && !strings.Contains(lower, "sandbox"):
		// Pure presentation styles are low risk unless mixed with other paths.
		return true
	}
	return false
}

func hasRiskPathSegment(value, segment string) bool {
	return slices.Contains(strings.Split(strings.Trim(value, "/"), "/"), segment)
}

func memoryOnlyMutation(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "remember", "forget":
		return true
	default:
		return false
	}
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
