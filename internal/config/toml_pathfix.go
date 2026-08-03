package config

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// TOMLEscapeFix describes one high-confidence repair of a Windows absolute
// path written with unescaped backslashes in a TOML config file. Only the
// offending token is replaced; nothing else in the document is reformatted.
type TOMLEscapeFix struct {
	Field      string `json:"field"` // dotted key path, e.g. plugins[0].command
	Line       int    `json:"line"`  // 1-based
	Column     int    `json:"column"`
	RawToken   string `json:"rawToken"`   // the original quoted token, e.g. "D:\new\tool"
	FixedToken string `json:"fixedToken"` // the repaired quoted token
	Reason     string `json:"reason"`     // "invalid_escape" (document cannot parse) or "semantic_escape" (parses but decodes differently)
}

var windowsDriveTokenPattern = regexp.MustCompile(`^[A-Za-z]:\\`)
var windowsUNCTokenPattern = regexp.MustCompile(`^\\\\`)

// ScanTOMLPathEscapes lexes a TOML document and reports Windows absolute
// paths written with unescaped backslashes. Two problem classes are handled:
//
//   - invalid escapes that make the document unparseable, e.g. `"D:\开发"`;
//   - escapes that parse but silently change the value, e.g. `"D:\new\tool"`
//     where `\n` decodes as a newline.
//
// A token is only reported when every safety condition holds:
//
//   - the key is a known path field, or the value itself matches a drive
//     letter or UNC absolute path (this covers plugin args and env values);
//   - the string is a TOML single-line basic string with exact boundaries;
//   - the token contains at least one unescaped backslash separator;
//   - the repaired document parses, and every reported fix decodes to the
//     literal path the user wrote.
//
// Multi-line strings, ambiguous boundaries (for example a trailing backslash
// before the closing quote), non-path `\n`/`\t` escapes and documents with
// other syntax errors are never reported. Already-escaped backslashes
// (`D:\\new`) are never modified.
func ScanTOMLPathEscapes(body string) ([]TOMLEscapeFix, error) {
	fixes := scanTOMLPathEscapeCandidates(body)
	if len(fixes) == 0 {
		return nil, nil
	}
	reason := "semantic_escape"
	if _, err := decodeTOMLBytes([]byte(body), Default()); err != nil {
		reason = "invalid_escape"
	}
	for i := range fixes {
		fixes[i].Reason = reason
	}
	return verifyTOMLPathFixes(body, fixes)
}

// ApplyTOMLPathEscapes applies the given fixes to body and re-verifies that
// the repaired document parses and every fix decodes to its literal path.
func ApplyTOMLPathEscapes(body string, fixes []TOMLEscapeFix) (string, error) {
	if len(fixes) == 0 {
		return body, nil
	}
	verified, err := verifyTOMLPathFixes(body, fixes)
	if err != nil {
		return "", err
	}
	if len(verified) == 0 {
		return body, nil
	}
	return applyTOMLPathFixes(body, verified), nil
}

// scanTOMLPathEscapeCandidates performs the lexical scan without verification.
func scanTOMLPathEscapeCandidates(body string) []TOMLEscapeFix {
	s := &tomlPathScanner{body: body, line: 1, arrayCounts: map[string]int{}}
	var fixes []TOMLEscapeFix
	s.scan(func(fx TOMLEscapeFix) { fixes = append(fixes, fx) })
	return fixes
}

// verifyTOMLPathFixes applies the fixes, requires the repaired document to
// parse, and keeps only fixes that decode to the literal path the user wrote.
// Verification decodes the document into its raw TOML tree (config files may
// carry keys the Config struct does not model, e.g. a top-level command).
// Fixes that cannot be verified are dropped; if the document still fails to
// parse, an error is returned so callers fall back to quarantine or snapshot
// restore instead of writing anything.
func verifyTOMLPathFixes(body string, fixes []TOMLEscapeFix) ([]TOMLEscapeFix, error) {
	for {
		fixed := applyTOMLPathFixes(body, fixes)
		var raw map[string]any
		if _, err := decodeTOMLBytes([]byte(fixed), &raw); err != nil {
			return nil, fmt.Errorf("TOML path escape repair: repaired document still does not parse: %w", err)
		}
		var kept []TOMLEscapeFix
		for _, fx := range fixes {
			content := strings.TrimSuffix(strings.TrimPrefix(fx.RawToken, `"`), `"`)
			// The expected semantics are the value as written: single
			// backslashes are literal path separators, and already-escaped
			// `\\` pairs keep their TOML meaning (one literal backslash).
			want := intendedPathLiteral(content)
			if got, ok := rawTOMLFieldValue(raw, fx.Field); ok && fmt.Sprintf("%v", got) == want {
				kept = append(kept, fx)
			}
		}
		if len(kept) == len(fixes) {
			return kept, nil
		}
		if len(kept) == 0 {
			return nil, fmt.Errorf("TOML path escape repair: none of the %d candidate fixes verified semantically", len(fixes))
		}
		fixes = kept
	}
}

// rawTOMLFieldValue walks a raw TOML tree and returns the value at a dotted
// field path with [i] slice indices, e.g. plugins[0].env.GODOT_PATH.
func rawTOMLFieldValue(node any, path string) (any, bool) {
	segments, ok := splitTOMLFieldPath(path)
	if !ok {
		return nil, false
	}
	cur := node
	for _, seg := range segments {
		if idx, isIdx := seg.index(); isIdx {
			switch list := cur.(type) {
			case []any:
				if idx >= len(list) {
					return nil, false
				}
				cur = list[idx]
			case []map[string]any:
				if idx >= len(list) {
					return nil, false
				}
				cur = list[idx]
			default:
				return nil, false
			}
			continue
		}
		table, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := table[seg.name]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

type tomlFieldSegment struct {
	name string
	idx  int
}

func (s tomlFieldSegment) index() (int, bool) {
	if s.idx >= 0 {
		return s.idx, true
	}
	return 0, false
}

// splitTOMLFieldPath parses paths like `plugins[0].env.GODOT_PATH` into
// segments. A bracketed part (`plugins[0]`) produces two segments: a map
// lookup by name followed by an index, matching how TOML decodes array tables.
func splitTOMLFieldPath(path string) ([]tomlFieldSegment, bool) {
	var segs []tomlFieldSegment
	for _, part := range strings.Split(path, ".") {
		for {
			idx := strings.IndexByte(part, '[')
			if idx < 0 || !strings.HasSuffix(part, "]") {
				break
			}
			name := part[:idx]
			if name != "" {
				segs = append(segs, tomlFieldSegment{name: name, idx: -1})
			}
			num, err := strconv.Atoi(part[idx+1 : len(part)-1])
			if err != nil {
				return nil, false
			}
			segs = append(segs, tomlFieldSegment{name: "", idx: num})
			break
		}
		if strings.IndexByte(part, '[') < 0 {
			segs = append(segs, tomlFieldSegment{name: part, idx: -1})
		}
	}
	return segs, true
}

// applyTOMLPathFixes replaces each fix's original token with its fixed token.
// All positions are resolved against the original body first and applied from
// the end, so replacements earlier on the same line never invalidate the
// remaining positions. Tokens never overlap, so a single descending pass
// suffices.
func applyTOMLPathFixes(body string, fixes []TOMLEscapeFix) string {
	type applied struct {
		start int
		raw   string
		fixed string
	}
	var list []applied
	for _, fx := range fixes {
		start := lineColumnOffset(body, fx.Line, fx.Column)
		if start < 0 || start+len(fx.RawToken) > len(body) || body[start:start+len(fx.RawToken)] != fx.RawToken {
			continue // position no longer matches (concurrent edit); skip
		}
		list = append(list, applied{start: start, raw: fx.RawToken, fixed: fx.FixedToken})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].start > list[j].start })
	out := body
	for _, ap := range list {
		out = out[:ap.start] + ap.fixed + out[ap.start+len(ap.raw):]
	}
	return out
}

func lineColumnOffset(body string, line, col int) int {
	current := 1
	start := 0
	for i := 0; i < len(body); i++ {
		if current == line {
			return start + col - 1
		}
		if body[i] == '\n' {
			current++
			start = i + 1
		}
	}
	return -1
}

func hasSingleBackslash(content string) bool {
	for i := 0; i < len(content); i++ {
		if content[i] != '\\' {
			continue
		}
		if i+1 < len(content) && content[i+1] == '\\' {
			i++
			continue
		}
		return true
	}
	return false
}

// intendedPathLiteral computes the value a Windows path token means as
// written: single backslashes are literal path separators and already-escaped
// `\\` pairs keep their TOML semantics (one literal backslash). A leading `\\`
// is a UNC prefix and stays two literal backslashes so a repaired UNC path
// remains a valid network path. The repaired token must decode to exactly
// this value.
func intendedPathLiteral(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	prefix := uncPrefixLen(content)
	if prefix > 0 {
		b.WriteString(`\\`)
	}
	for i := prefix; i < len(content); i++ {
		if content[i] == '\\' {
			if i+1 < len(content) && content[i+1] == '\\' {
				b.WriteByte('\\')
				i++
				continue
			}
			b.WriteByte('\\')
			continue
		}
		b.WriteByte(content[i])
	}
	return b.String()
}

// uncPrefixLen returns 2 when content starts with `\\` (a UNC prefix written
// as an escaped backslash pair), else 0.
func uncPrefixLen(content string) int {
	if strings.HasPrefix(content, `\\`) {
		return 2
	}
	return 0
}

// doubleSingleBackslashes doubles every backslash that is not already part of
// a `\\` pair, leaving already-escaped backslashes untouched. A leading `\\`
// UNC prefix is emitted as `\\\\` so it decodes back to two literal
// backslashes.
func doubleSingleBackslashes(content string) string {
	var b strings.Builder
	b.Grow(len(content))
	prefix := uncPrefixLen(content)
	if prefix > 0 {
		b.WriteString(`\\\\`)
	}
	for i := prefix; i < len(content); i++ {
		if content[i] == '\\' {
			if i+1 < len(content) && content[i+1] == '\\' {
				b.WriteString(`\\`)
				i++
				continue
			}
			b.WriteString(`\\`)
			continue
		}
		b.WriteByte(content[i])
	}
	return b.String()
}

func isTOMLPathCandidate(keyPath, content string) bool {
	leaf := keyPath
	if idx := strings.LastIndexByte(leaf, '.'); idx >= 0 {
		leaf = leaf[idx+1:]
	}
	leaf = strings.TrimSuffix(leaf, "]")
	if idx := strings.IndexByte(leaf, '['); idx >= 0 {
		leaf = leaf[:idx]
	}
	if isTOMLPathField(leaf) {
		return true
	}
	return windowsDriveTokenPattern.MatchString(content) || windowsUNCTokenPattern.MatchString(content)
}

// tomlPathScanner is a small TOML lexer that walks the document tracking key
// paths, line and column, and collects basic-string tokens that may contain
// unescaped Windows path separators.
type tomlPathScanner struct {
	body        string
	pos         int
	line        int
	lineStart   int
	tablePath   []string
	arrayCounts map[string]int
}

func (s *tomlPathScanner) atEnd() bool { return s.pos >= len(s.body) }

func (s *tomlPathScanner) cur() byte { return s.body[s.pos] }

func (s *tomlPathScanner) advance() {
	if s.body[s.pos] == '\n' {
		s.line++
		s.lineStart = s.pos + 1
	}
	s.pos++
}

func (s *tomlPathScanner) column() int { return s.pos - s.lineStart + 1 }

func (s *tomlPathScanner) skipToEOL() {
	for !s.atEnd() && s.cur() != '\n' {
		s.advance()
	}
}

func (s *tomlPathScanner) atLineStart() bool { return s.pos == s.lineStart }

func (s *tomlPathScanner) scan(emit func(TOMLEscapeFix)) {
	for !s.atEnd() {
		switch s.cur() {
		case ' ', '\t', '\r', '\n':
			s.advance()
		case '#':
			s.skipToEOL()
		case '[':
			if s.atLineStart() {
				s.scanTableHeader()
				s.skipToEOL()
			} else {
				// inline array value on the current key line; handled by the
				// value scanner below — skip the bracket to keep scanning.
				s.advance()
			}
		default:
			s.scanKeyValue(emit)
			s.skipToEOL()
		}
	}
}

func (s *tomlPathScanner) scanTableHeader() {
	// consume '[' (and a second '[' for array tables)
	isArray := false
	s.advance()
	if !s.atEnd() && s.cur() == '[' {
		isArray = true
		s.advance()
	}
	segs := s.scanKeySegments()
	// skip until ']'
	for !s.atEnd() && s.cur() != ']' {
		s.advance()
	}
	if !s.atEnd() {
		s.advance()
	}
	if isArray && !s.atEnd() && s.cur() == ']' {
		s.advance()
	}
	// Only the last segment of a [[a.b.c]] header is an array table; the
	// earlier segments are plain table lookups.
	s.tablePath = nil
	for i, seg := range segs {
		if isArray && i == len(segs)-1 {
			key := strings.Join(append(append([]string(nil), s.tablePath...), seg), ".")
			idx := s.arrayCounts[key]
			s.arrayCounts[key] = idx + 1
			s.tablePath = append(s.tablePath, fmt.Sprintf("%s[%d]", seg, idx))
			continue
		}
		s.tablePath = append(s.tablePath, seg)
	}
}

// scanKeySegments parses a dotted key (bare or quoted segments) and returns
// the decoded segment names.
func (s *tomlPathScanner) scanKeySegments() []string {
	var segs []string
	for {
		s.skipSpaces()
		if s.atEnd() {
			break
		}
		var seg string
		switch c := s.cur(); c {
		case '"':
			seg, _ = s.scanBasicStringToken()
		case '\'':
			seg = s.scanLiteralStringToken()
		default:
			start := s.pos
			for !s.atEnd() && !strings.ContainsRune(".= \t\r\n[]", rune(s.cur())) {
				s.advance()
			}
			seg = s.body[start:s.pos]
		}
		if seg != "" {
			segs = append(segs, seg)
		}
		s.skipSpaces()
		if !s.atEnd() && s.cur() == '.' {
			s.advance()
			continue
		}
		break
	}
	return segs
}

func (s *tomlPathScanner) skipSpaces() {
	for !s.atEnd() && (s.cur() == ' ' || s.cur() == '\t' || s.cur() == '\r') {
		s.advance()
	}
}

// scanBasicStringToken scans a single-line basic string and returns the raw
// content (escape sequences preserved verbatim). An unclosed string returns
// ok=false.
func (s *tomlPathScanner) scanBasicStringToken() (string, bool) {
	// s.cur() == '"'
	s.advance()
	start := s.pos
	for !s.atEnd() {
		c := s.cur()
		if c == '"' {
			content := s.body[start:s.pos]
			s.advance()
			return content, true
		}
		if c == '\\' {
			s.advance()
			if s.atEnd() || s.cur() == '\n' {
				return "", false
			}
			s.advance()
			continue
		}
		if c == '\n' {
			return "", false
		}
		s.advance()
	}
	return "", false
}

func (s *tomlPathScanner) scanLiteralStringToken() string {
	// s.cur() == '\''
	s.advance()
	start := s.pos
	for !s.atEnd() && s.cur() != '\'' && s.cur() != '\n' {
		s.advance()
	}
	content := s.body[start:s.pos]
	if !s.atEnd() && s.cur() == '\'' {
		s.advance()
	}
	return content
}

// skipMultiLineString consumes a """ ... """ block (s.cur() at the opening
// quote run).
func (s *tomlPathScanner) skipMultiLineString() {
	for !s.atEnd() {
		if s.cur() == '"' {
			// count consecutive quotes
			n := 0
			for !s.atEnd() && s.cur() == '"' && n < 3 {
				s.advance()
				n++
			}
			if n >= 3 {
				return
			}
			continue
		}
		s.advance()
	}
}

func (s *tomlPathScanner) scanKeyValue(emit func(TOMLEscapeFix)) {
	segs := s.scanKeySegments()
	s.skipSpaces()
	if s.atEnd() || s.cur() != '=' {
		return
	}
	s.advance()
	s.skipSpaces()
	keyPath := strings.Join(append(append([]string(nil), s.tablePath...), segs...), ".")
	s.scanValue(keyPath, emit)
}

// scanValue parses a value starting at the current position, emitting string
// candidates. Inline tables and arrays are scanned recursively.
func (s *tomlPathScanner) scanValue(keyPath string, emit func(TOMLEscapeFix)) {
	if s.atEnd() {
		return
	}
	switch c := s.cur(); c {
	case '"':
		if s.pos+2 < len(s.body) && s.body[s.pos+1] == '"' && s.body[s.pos+2] == '"' {
			s.skipMultiLineString()
			return
		}
		line, col := s.line, s.column()
		content, ok := s.scanBasicStringToken()
		if !ok {
			return // unclosed single-line string; boundaries ambiguous
		}
		if isTOMLPathCandidate(keyPath, content) && hasSingleBackslash(content) {
			rawToken := `"` + content + `"`
			emit(TOMLEscapeFix{
				Field:      keyPath,
				Line:       line,
				Column:     col,
				RawToken:   rawToken,
				FixedToken: `"` + doubleSingleBackslashes(content) + `"`,
			})
		}
	case '\'':
		if s.pos+2 < len(s.body) && s.body[s.pos+1] == '\'' && s.body[s.pos+2] == '\'' {
			s.skipMultiLineString()
			return
		}
		s.scanLiteralStringToken()
	case '{':
		s.scanInlineTable(keyPath, emit)
	case '[':
		s.scanInlineArray(keyPath, emit)
	default:
		// scalar: skip to whitespace / comment / newline / inline delimiter
		for !s.atEnd() && !strings.ContainsRune(" \t\r\n,]}", rune(s.cur())) {
			s.advance()
		}
	}
}

func (s *tomlPathScanner) scanInlineTable(base string, emit func(TOMLEscapeFix)) {
	s.advance() // '{'
	for !s.atEnd() {
		start := s.pos
		s.skipSpaces()
		if s.atEnd() {
			return
		}
		switch s.cur() {
		case '#':
			s.skipToEOL()
		case '}':
			s.advance()
			return
		case '\n':
			s.advance()
		case ',':
			s.advance()
		default:
			segs := s.scanKeySegments()
			s.skipSpaces()
			if len(segs) == 0 {
				// Malformed inline content (e.g. a bare `=`); fall through to
				// the progress guard instead of looping on the same character.
				break
			}
			if s.atEnd() || s.cur() != '=' {
				break
			}
			s.advance()
			s.skipSpaces()
			keyPath := base
			for _, seg := range segs {
				keyPath = keyPath + "." + seg
			}
			s.scanValue(keyPath, emit)
		}
		if s.pos == start {
			s.advance() // malformed input: guarantee forward progress
		}
	}
}

func (s *tomlPathScanner) scanInlineArray(base string, emit func(TOMLEscapeFix)) {
	s.advance() // '['
	idx := 0
	for !s.atEnd() {
		start := s.pos
		s.skipSpaces()
		if s.atEnd() {
			return
		}
		switch s.cur() {
		case '#':
			s.skipToEOL()
		case ']':
			s.advance()
			return
		case '\n':
			s.advance()
		case ',':
			s.advance()
			idx++
		default:
			s.scanValue(fmt.Sprintf("%s[%d]", base, idx), emit)
		}
		if s.pos == start {
			s.advance() // malformed input: guarantee forward progress
		}
	}
}
