package evidence

import (
	"path"
	"slices"
	"strings"
	"unicode"
)

// PathClass is a structured category for one filesystem target.
type PathClass uint8

const (
	PathOrdinary PathClass = iota
	PathDocs
	PathI18n
	PathTest
	PathStyle
	PathSchema
	PathMigration
	PathAuth
	PathSecret
	PathPublicAPI
	PathConcurrency
)

// ClassifyPath reports the strongest structured class for a workspace path.
// Matching uses directory segments, filename tokens, and extensions — never a
// raw substring of the whole path, so author.go cannot match auth.
func ClassifyPath(p, workspaceRoot string) PathClass {
	relevant := riskRelevantPath(p, workspaceRoot)
	if relevant == "" || relevant == "." {
		return PathOrdinary
	}
	lower := strings.ToLower(strings.ReplaceAll(relevant, `\`, "/"))
	segments := pathSegments(lower)
	base := ""
	if len(segments) > 0 {
		base = segments[len(segments)-1]
	}
	ext := strings.ToLower(path.Ext(base))
	stem := strings.TrimSuffix(base, ext)
	tokens := filenameTokens(stem)

	switch {
	case classMatches(segments, tokens, ext, authPathMatch):
		return PathAuth
	case classMatches(segments, tokens, ext, secretPathMatch):
		return PathSecret
	case classMatches(segments, tokens, ext, migrationPathMatch):
		return PathMigration
	case classMatches(segments, tokens, ext, schemaPathMatch):
		return PathSchema
	case classMatches(segments, tokens, ext, publicAPIPathMatch):
		return PathPublicAPI
	case pathLooksLowRisk(p, workspaceRoot) && classMatches(segments, tokens, ext, stylePathMatch):
		return PathStyle
	case pathLooksLowRisk(p, workspaceRoot) && classMatches(segments, tokens, ext, i18nPathMatch):
		return PathI18n
	case pathLooksLowRisk(p, workspaceRoot) && classMatches(segments, tokens, ext, testPathMatch):
		return PathTest
	case pathLooksLowRisk(p, workspaceRoot) && classMatches(segments, tokens, ext, docsPathMatch):
		return PathDocs
	case pathLooksLowRisk(p, workspaceRoot):
		return PathDocs
	case classMatches(segments, tokens, ext, concurrencyPathMatch):
		return PathConcurrency
	default:
		return PathOrdinary
	}
}

type pathMatch struct {
	segments   []string
	tokens     []string
	exts       []string
	testSuffix bool
}

var (
	authPathMatch = pathMatch{
		segments: []string{"auth", "authentication", "authorization", "permission", "permissions", "oauth", "oidc", "sso", "iam", "acl", "rbac"},
		tokens:   []string{"auth", "authentication", "authorization", "permission", "permissions", "oauth", "oidc", "sso", "session", "login", "passwd"},
	}
	secretPathMatch = pathMatch{
		segments: []string{"secret", "secrets", "credential", "credentials", "keystore", "keyring", "certs", "tls", "ssl"},
		tokens:   []string{"secret", "secrets", "credential", "credentials", "password", "passwd", "token", "keystore", "keyring"},
	}
	migrationPathMatch = pathMatch{
		segments: []string{"migration", "migrations", "migrate"},
		tokens:   []string{"migration", "migrate"},
	}
	schemaPathMatch = pathMatch{
		segments: []string{"schema", "schemas", "protobuf", "proto", "openapi", "graphql"},
		tokens:   []string{"schema", "migration"},
		exts:     []string{".proto", ".graphql"},
	}
	publicAPIPathMatch = pathMatch{
		segments: []string{"api", "apis", "openapi", "swagger"},
		tokens:   []string{"openapi", "swagger"},
		exts:     []string{".proto"},
	}
	docsPathMatch = pathMatch{
		segments: []string{"docs", "doc", "documentation"},
		exts:     []string{".md", ".mdx", ".txt", ".rst"},
	}
	i18nPathMatch = pathMatch{
		segments: []string{"i18n", "locales", "locale", "l10n"},
	}
	testPathMatch = pathMatch{
		segments:   []string{"testdata", "__tests__", "fixtures", "fixture"},
		testSuffix: true,
	}
	stylePathMatch = pathMatch{
		exts: []string{".css", ".scss", ".sass", ".less"},
	}
	concurrencyPathMatch = pathMatch{
		segments: []string{"concurrent", "concurrency", "goroutine"},
		tokens:   []string{"mutex", "rwmutex", "rwlock", "concurrent", "concurrency", "goroutine", "waitgroup", "atomic", "deadlock", "race"},
	}
)

func classMatches(segments, tokens []string, ext string, m pathMatch) bool {
	for _, seg := range segments {
		if containsWord(m.segments, seg) {
			return true
		}
	}
	for _, tok := range tokens {
		if containsWord(m.tokens, tok) {
			return true
		}
	}
	if containsWord(m.exts, ext) {
		return true
	}
	if m.testSuffix && isTestFilename(segments) {
		return true
	}
	return false
}

func containsWord(words []string, got string) bool {
	return slices.Contains(words, got)
}

func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" || p == "." {
		return nil
	}
	return strings.Split(p, "/")
}

func filenameTokens(stem string) []string {
	if stem == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, strings.ToLower(b.String()))
		b.Reset()
	}
	for _, r := range stem {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// PathModule is the top-level package/module key used for architecture scope.
// internal/agent/foo.go and cmd/reasonix/main.go are different modules.
func PathModule(p, workspaceRoot string) string {
	relevant := riskRelevantPath(p, workspaceRoot)
	if relevant == "" || relevant == "." {
		return ""
	}
	lower := strings.ToLower(strings.ReplaceAll(relevant, `\`, "/"))
	segments := pathSegments(lower)
	if len(segments) == 0 {
		return ""
	}
	switch segments[0] {
	case "internal", "cmd", "desktop", "tools", "docs":
		if len(segments) >= 2 {
			return segments[0] + "/" + segments[1]
		}
	}
	return segments[0]
}

func isTestFilename(segments []string) bool {
	if len(segments) == 0 {
		return false
	}
	base := segments[len(segments)-1]
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, "_test.ts") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, "_spec.ts") ||
		strings.HasSuffix(base, ".spec.ts")
}

// pathLooksSensitive reports whether a path is an auth, secret, schema, or
// migration surface. User prompt text is never consulted.
func pathLooksSensitive(p, workspaceRoot string) bool {
	switch ClassifyPath(p, workspaceRoot) {
	case PathAuth, PathSecret, PathSchema, PathMigration, PathPublicAPI:
		return true
	default:
		return false
	}
}
