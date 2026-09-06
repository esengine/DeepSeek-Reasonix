package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

const deepSeekOfficialBalanceURL = "https://api.deepseek.com/user/balance"

// MigrateLegacyDeepSeekProtocolUserConfig upgrades only unmodified legacy
// DeepSeek provider aliases in the user-global config. It deliberately edits
// the original TOML in place instead of rendering Config, so comments, future
// fields, and unrelated provider blocks survive byte-for-byte.
func MigrateLegacyDeepSeekProtocolUserConfig() (bool, error) {
	path := userConfigLoadPath()
	if strings.TrimSpace(path) == "" {
		return false, nil
	}
	return editLegacyDeepSeekProtocolFile(path, "", true)
}

// IsDeepSeekProtocolConfigParseError reports whether migration failed while
// parsing the user configuration rather than reading, locking, or writing it.
func IsDeepSeekProtocolConfigParseError(err error) bool {
	var parseErr toml.ParseError
	return errors.As(err, &parseErr)
}

// UpgradeDeepSeekProviderProtocol switches one official DeepSeek provider
// family to Anthropic Messages after an explicit user action. Passing the
// canonical name "deepseek" upgrades matching canonical/legacy alias blocks.
func UpgradeDeepSeekProviderProtocol(path, name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("upgrade DeepSeek protocol: empty provider name")
	}
	return editLegacyDeepSeekProtocolFile(path, name, false)
}

// UpgradeDeepSeekProviderProtocolUserConfig applies the explicit upgrade to
// the active user-global source, including a legacy config location.
func UpgradeDeepSeekProviderProtocolUserConfig(name string) (bool, error) {
	return UpgradeDeepSeekProviderProtocol(userConfigLoadPath(), name)
}

// CanUpgradeDeepSeekProviderProtocolUserConfig reports whether the active
// user-global source contains a safely mappable provider in the requested
// DeepSeek family. Settings uses the same rewrite parser as the mutation path,
// so a project-only provider or an unsupported TOML shape cannot expose an
// action that would later edit a different file or fail unexpectedly.
func CanUpgradeDeepSeekProviderProtocolUserConfig(name string) bool {
	path := userConfigLoadPath()
	if strings.TrimSpace(path) == "" {
		return false
	}
	resolved, exists, err := statConfigPath(path)
	if err != nil || !exists {
		return false
	}
	raw, err := fileencoding.ReadFileUTF8(resolved)
	if err != nil {
		return false
	}
	_, changed, err := rewriteLegacyDeepSeekProtocol(string(raw), name, false)
	return err == nil && changed
}

// CanUpgradeDeepSeekProviderProtocol reports whether Settings may offer the
// explicit protocol upgrade. Custom transport/capability fields prevent the
// automatic migration but remain preserved when the user confirms this action.
func CanUpgradeDeepSeekProviderProtocol(p *ProviderEntry) bool {
	if p == nil || !strings.EqualFold(strings.TrimSpace(p.Kind), "openai") ||
		!isOfficialDeepSeekOpenAIEndpoint(p.BaseURL) ||
		strings.TrimSpace(p.APIKeyEnv) == "" {
		return false
	}
	models := p.ModelList()
	switch strings.TrimSpace(p.Name) {
	case "deepseek-flash":
		return len(models) == 1 && strings.TrimSpace(models[0]) == "deepseek-v4-flash"
	case "deepseek-pro":
		return len(models) == 1 && strings.TrimSpace(models[0]) == "deepseek-v4-pro"
	case "deepseek":
		if len(models) == 0 {
			return false
		}
		for _, model := range models {
			switch strings.TrimSpace(model) {
			case "deepseek-v4-flash", "deepseek-v4-pro":
			default:
				return false
			}
		}
		return true
	default:
		return false
	}
}

func editLegacyDeepSeekProtocolFile(path, target string, automatic bool) (bool, error) {
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return false, err
	}
	defer unlock()

	resolved, exists, err := statConfigPath(path)
	if err != nil || !exists {
		return false, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false, err
	}
	rawBytes, err := os.ReadFile(resolved)
	if err != nil {
		return false, err
	}
	encoding, detected := fileencoding.Detect(rawBytes)
	raw := fileencoding.Decode(detected, encoding)
	next, changed, err := rewriteLegacyDeepSeekProtocol(string(raw), target, automatic)
	if err != nil || !changed {
		return changed, err
	}
	if err := fileutil.AtomicWriteFile(resolved, fileencoding.Encode(next, encoding), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func rewriteLegacyDeepSeekProtocol(raw, target string, automatic bool) (string, bool, error) {
	// Retained for compatibility callers; current startup no longer invokes
	// the old automatic Messages migration, and v8 choices must stay untouched.
	if automatic {
		var header struct {
			ConfigVersion int `toml:"config_version"`
		}
		if _, err := toml.Decode(raw, &header); err != nil {
			return raw, false, err
		}
		if header.ConfigVersion >= deepSeekChatDefaultConfigVersion {
			return raw, false, nil
		}
	}
	return rewriteDeepSeekProtocol(raw, "anthropic", deepSeekAnthropicBaseURL, func(entry *ProviderEntry, fields map[string]any) bool {
		if !CanUpgradeDeepSeekProviderProtocol(entry) {
			return false
		}
		if automatic {
			return isUnmodifiedLegacyDeepSeekProvider(*entry, fields)
		}
		return deepSeekUpgradeTargetMatches(target, entry.Name)
	})
}

// Shared lexical rewrite preserves comments, unknown fields and inline tables.
func rewriteDeepSeekProtocol(raw, kind, baseURL string, eligible func(*ProviderEntry, map[string]any) bool) (string, bool, error) {
	var decoded struct {
		Providers []ProviderEntry `toml:"providers"`
	}
	if _, err := toml.Decode(raw, &decoded); err != nil {
		return raw, false, err
	}
	var generic struct {
		Providers []map[string]any `toml:"providers"`
	}
	if _, err := toml.Decode(raw, &generic); err != nil {
		return raw, false, err
	}

	lines := strings.Split(raw, "\n")
	blocks := providerTOMLBlocks(lines)
	if len(blocks) == len(decoded.Providers) && len(generic.Providers) == len(decoded.Providers) {
		changed := false
		for i := range decoded.Providers {
			entry := &decoded.Providers[i]
			if !eligible(entry, generic.Providers[i]) {
				continue
			}
			if err := rewriteDeepSeekProviderBlockAs(lines, blocks[i], kind, baseURL); err != nil {
				return raw, false, err
			}
			changed = true
		}
		return strings.Join(lines, "\n"), changed, nil
	}

	inlineBlocks, err := providerTOMLInlineBlocks(raw)
	if err != nil || len(inlineBlocks) != len(decoded.Providers) || len(generic.Providers) != len(decoded.Providers) {
		return raw, false, fmt.Errorf("upgrade DeepSeek protocol: could not map provider tables safely")
	}
	replacements := make([]tomlReplacement, 0, len(decoded.Providers)*2)
	for i := range decoded.Providers {
		entry := &decoded.Providers[i]
		if !eligible(entry, generic.Providers[i]) {
			continue
		}
		block := inlineBlocks[i]
		if block.kindStart < 0 || block.baseURLStart < 0 {
			return raw, false, fmt.Errorf("upgrade DeepSeek protocol: inline provider table is missing kind or base_url")
		}
		replacements = append(replacements,
			tomlReplacement{start: block.kindStart, end: block.kindEnd, value: strconv.Quote(kind)},
			tomlReplacement{start: block.baseURLStart, end: block.baseURLEnd, value: strconv.Quote(baseURL)},
		)
	}
	if len(replacements) == 0 {
		return raw, false, nil
	}
	return applyTOMLReplacements(raw, replacements), true, nil
}

func isUnmodifiedLegacyDeepSeekProvider(p ProviderEntry, raw map[string]any) bool {
	if p.Name != "deepseek-flash" && p.Name != "deepseek-pro" {
		return false
	}
	if !isExactDeepSeekOpenAIEndpoint(p.BaseURL) {
		return false
	}
	// Automatic migration is intentionally narrower than the explicit Settings
	// upgrade: only the stock environment variable is unambiguous enough to
	// change without user confirmation.
	if strings.TrimSpace(p.APIKeyEnv) != "DEEPSEEK_API_KEY" {
		return false
	}
	allowed := map[string]bool{
		"name": true, "kind": true, "base_url": true, "model": true,
		"api_key_env": true, "balance_url": true, "context_window": true,
		"price": true,
	}
	for key := range raw {
		if !allowed[key] {
			return false
		}
	}
	for _, required := range []string{"name", "kind", "base_url", "model", "api_key_env"} {
		if _, ok := raw[required]; !ok {
			return false
		}
	}
	if p.BalanceURL != "" && strings.TrimRight(strings.TrimSpace(p.BalanceURL), "/") != deepSeekOfficialBalanceURL {
		return false
	}
	if p.ContextWindow != 0 && p.ContextWindow != 1_000_000 {
		return false
	}
	return p.Price == nil || IsKnownDeepSeekOfficialPricing(p.Model, p.Price)
}

func deepSeekUpgradeTargetMatches(target, providerName string) bool {
	target = strings.TrimSpace(target)
	providerName = strings.TrimSpace(providerName)
	if target == providerName {
		return true
	}
	if CanonicalDesktopOfficialProviderName(target) != "deepseek" {
		return false
	}
	return CanonicalDesktopOfficialProviderName(providerName) == "deepseek"
}

func isExactDeepSeekOpenAIEndpoint(raw string) bool {
	path, ok := deepSeekOpenAIEndpointPath(raw)
	return ok && path == ""
}

func isOfficialDeepSeekOpenAIEndpoint(raw string) bool {
	path, ok := deepSeekOpenAIEndpointPath(raw)
	return ok && (path == "" || path == "/v1")
}

func deepSeekOpenAIEndpointPath(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") ||
		!strings.EqualFold(u.Hostname(), "api.deepseek.com") || u.Port() != "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.TrimRight(u.EscapedPath(), "/"), true
}

type providerTOMLBlock struct {
	start int
	end   int
}

func providerTOMLBlocks(lines []string) []providerTOMLBlock {
	headerLines := make([]int, 0)
	providerStarts := make([]int, 0)
	state := tomlOutside
	for i, line := range lines {
		if state != tomlOutside {
			state = advanceTOMLStringState(state, line)
			continue
		}
		if tomlSectionHeader(line) != "" {
			headerLines = append(headerLines, i)
			if isProviderArrayTableHeader(line) {
				providerStarts = append(providerStarts, i)
			}
		}
		state = advanceTOMLStringState(tomlOutside, line)
	}
	out := make([]providerTOMLBlock, 0, len(providerStarts))
	for _, start := range providerStarts {
		end := len(lines)
		for _, header := range headerLines {
			if header > start {
				end = header
				break
			}
		}
		out = append(out, providerTOMLBlock{start: start, end: end})
	}
	return out
}

type providerTOMLInlineBlock struct {
	start, end               int
	kindStart, kindEnd       int
	baseURLStart, baseURLEnd int
}

type tomlReplacement struct {
	start, end int
	value      string
}

// providerTOMLInlineBlocks locates providers declared as an inline TOML array
// while preserving byte offsets so migration can edit only two scalar values.
// The parser is deliberately lexical: BurntSushi/toml validates the document,
// while this scan handles nested arrays/tables and quoted delimiters without
// re-rendering comments or unknown fields.
func providerTOMLInlineBlocks(raw string) ([]providerTOMLInlineBlock, error) {
	arrayStart, arrayEnd, err := providerTOMLInlineArrayRange(raw)
	if err != nil {
		return nil, err
	}
	return collectProviderTOMLInlineBlocks(raw, arrayStart, arrayEnd)
}

func providerTOMLInlineArrayRange(raw string) (int, int, error) {
	arrayStart, arrayEnd := -1, -1
	section := ""
	state := tomlOutside
	for _, span := range tomlLineSpans(raw) {
		if state != tomlOutside {
			state = advanceTOMLStringState(state, span.text)
			continue
		}
		if header := tomlSectionHeader(span.text); header != "" {
			section = header
			state = advanceTOMLStringState(tomlOutside, span.text)
			continue
		}
		if section != "" {
			state = advanceTOMLStringState(tomlOutside, span.text)
			continue
		}
		line := strings.TrimRight(span.text, "\r\n")
		nextState := advanceTOMLStringState(tomlOutside, line)
		key, _, ok := tomlKeyValue(line)
		if !ok || strings.Trim(key, `"'`) != "providers" {
			state = nextState
			continue
		}
		equals := strings.IndexByte(line, '=')
		valueStart := span.start + equals + 1
		for valueStart < len(raw) && (raw[valueStart] == ' ' || raw[valueStart] == '\t' || raw[valueStart] == '\r' || raw[valueStart] == '\n') {
			valueStart++
		}
		if valueStart >= len(raw) || raw[valueStart] != '[' {
			state = nextState
			continue
		}
		valueEnd, err := scanTOMLDelimitedValue(raw, valueStart, '[', ']')
		if err != nil {
			return -1, -1, err
		}
		arrayStart, arrayEnd = valueStart, valueEnd
		break
	}
	if arrayStart < 0 {
		return -1, -1, fmt.Errorf("providers inline array not found")
	}
	return arrayStart, arrayEnd, nil
}

func collectProviderTOMLInlineBlocks(raw string, arrayStart, arrayEnd int) ([]providerTOMLInlineBlock, error) {
	var tables []providerTOMLInlineBlock
	stack := make([]byte, 0, 4)
	tableStart := -1
	var scanErr error
	err := scanTOMLOutsideStrings(raw, arrayStart, arrayEnd+1, func(pos int, ch byte) bool {
		if scanErr != nil {
			return false
		}
		switch ch {
		case '[', '{':
			stack = append(stack, ch)
			if ch == '{' && len(stack) == 2 && stack[0] == '[' {
				tableStart = pos
			}
		case ']', '}':
			if len(stack) == 0 || (ch == ']' && stack[len(stack)-1] != '[') || (ch == '}' && stack[len(stack)-1] != '{') {
				scanErr = fmt.Errorf("invalid providers inline array nesting")
				return false
			}
			if ch == '}' && len(stack) == 2 && tableStart >= 0 {
				block, err := parseProviderTOMLInlineBlock(raw, tableStart, pos)
				if err != nil {
					scanErr = err
					return false
				}
				tables = append(tables, block)
				tableStart = -1
			}
			stack = stack[:len(stack)-1]
		}
		return true
	})
	if scanErr != nil {
		return nil, scanErr
	}
	if err != nil {
		return nil, err
	}
	if len(stack) != 0 || len(tables) == 0 {
		return nil, fmt.Errorf("providers inline array contains no provider tables")
	}
	return tables, nil
}

func parseProviderTOMLInlineBlock(raw string, start, end int) (providerTOMLInlineBlock, error) {
	block := providerTOMLInlineBlock{start: start, end: end, kindStart: -1, baseURLStart: -1}
	segmentStart := start + 1
	depth := 0
	var segments [][2]int
	var scanErr error
	err := scanTOMLOutsideStrings(raw, start+1, end, func(pos int, ch byte) bool {
		if scanErr != nil {
			return false
		}
		switch ch {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth < 0 {
				scanErr = fmt.Errorf("invalid inline provider table nesting")
				return false
			}
		case ',':
			if depth == 0 {
				segments = append(segments, [2]int{segmentStart, pos})
				segmentStart = pos + 1
			}
		}
		return true
	})
	if scanErr != nil {
		return block, scanErr
	}
	if err != nil {
		return block, err
	}
	segments = append(segments, [2]int{segmentStart, end})
	for _, segment := range segments {
		start, end := trimTOMLWhitespace(raw, segment[0], segment[1])
		if start >= end {
			continue
		}
		equals, err := findTOMLAssignmentEquals(raw, start, end)
		if err != nil {
			return block, err
		}
		if equals < 0 {
			return block, fmt.Errorf("inline provider table contains a value without a key")
		}
		key := strings.Trim(strings.TrimSpace(raw[start:equals]), `"'`)
		valueStart, valueEnd := trimTOMLWhitespace(raw, equals+1, end)
		if comment := tomlInlineCommentIndex(raw[valueStart:valueEnd]); comment >= 0 {
			valueEnd = valueStart + comment
			valueStart, valueEnd = trimTOMLWhitespace(raw, valueStart, valueEnd)
		}
		switch key {
		case "kind":
			block.kindStart, block.kindEnd = valueStart, valueEnd
		case "base_url":
			block.baseURLStart, block.baseURLEnd = valueStart, valueEnd
		}
	}
	return block, nil
}

func scanTOMLDelimitedValue(raw string, start int, open, close byte) (int, error) {
	depth := 0
	end := -1
	var scanErr error
	err := scanTOMLOutsideStrings(raw, start, len(raw), func(pos int, ch byte) bool {
		switch ch {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				end = pos
				return false
			}
			if depth < 0 {
				scanErr = fmt.Errorf("invalid TOML array nesting")
				return false
			}
		}
		return true
	})
	if scanErr != nil {
		return -1, scanErr
	}
	if err != nil {
		return -1, err
	}
	if end < 0 {
		return -1, fmt.Errorf("unterminated TOML inline array")
	}
	return end, nil
}

// scanTOMLOutsideStrings visits structural bytes outside TOML strings and
// comments. It is used only after BurntSushi/toml has validated the document.
func scanTOMLOutsideStrings(raw string, start, end int, visit func(int, byte) bool) error {
	const (
		outside = iota
		basic
		literal
		multilineBasic
		multilineLiteral
	)
	state, escaped := outside, false
	for i := start; i < end; {
		ch := raw[i]
		switch state {
		case basic:
			if escaped {
				escaped = false
				i++
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				state = outside
			}
			i++
		case literal:
			if ch == '\'' {
				state = outside
			}
			i++
		case multilineBasic:
			if escaped {
				escaped = false
				i++
				continue
			}
			if ch == '\\' {
				escaped = true
				i++
				continue
			}
			if strings.HasPrefix(raw[i:], `"""`) {
				state = outside
				i += 3
				continue
			}
			i++
		case multilineLiteral:
			if strings.HasPrefix(raw[i:], "'''") {
				state = outside
				i += 3
				continue
			}
			i++
		default:
			if ch == '#' {
				for i < end && raw[i] != '\n' {
					i++
				}
				continue
			}
			if ch == '"' {
				run := 1
				for i+run < end && raw[i+run] == '"' {
					run++
				}
				if run >= 3 {
					state = multilineBasic
					i += 3
				} else {
					state = basic
					i++
				}
				continue
			}
			if ch == '\'' {
				run := 1
				for i+run < end && raw[i+run] == '\'' {
					run++
				}
				if run >= 3 {
					state = multilineLiteral
					i += 3
				} else {
					state = literal
					i++
				}
				continue
			}
			if visit != nil && !visit(i, ch) {
				return nil
			}
			i++
		}
	}
	if state != outside {
		return fmt.Errorf("unterminated TOML string")
	}
	return nil
}

func trimTOMLWhitespace(raw string, start, end int) (int, int) {
	for start < end && strings.ContainsRune(" \t\r\n", rune(raw[start])) {
		start++
	}
	for end > start && strings.ContainsRune(" \t\r\n", rune(raw[end-1])) {
		end--
	}
	return start, end
}

func findTOMLAssignmentEquals(raw string, start, end int) (int, error) {
	var found = -1
	depth := 0
	err := scanTOMLOutsideStrings(raw, start, end, func(pos int, ch byte) bool {
		switch ch {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case '=':
			if depth == 0 {
				found = pos
				return false
			}
		}
		return true
	})
	return found, err
}

func applyTOMLReplacements(raw string, replacements []tomlReplacement) string {
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	for _, r := range slices.Backward(replacements) {
		raw = raw[:r.start] + r.value + raw[r.end:]
	}
	return raw
}

func isProviderArrayTableHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	if comment := tomlInlineCommentIndex(trimmed); comment >= 0 {
		trimmed = strings.TrimSpace(trimmed[:comment])
	}
	if !strings.HasPrefix(trimmed, "[[") || !strings.HasSuffix(trimmed, "]]") {
		return false
	}
	key := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	switch {
	case key == "providers", key == "'providers'":
		return true
	case len(key) >= 2 && key[0] == '"' && key[len(key)-1] == '"':
		decoded, err := strconv.Unquote(key)
		return err == nil && decoded == "providers"
	default:
		return false
	}
}

func rewriteDeepSeekProviderBlockAs(lines []string, block providerTOMLBlock, kind, baseURL string) error {
	kindLine, baseURLLine := -1, -1
	state := tomlOutside
	for i := block.start + 1; i < block.end; i++ {
		if state != tomlOutside {
			state = advanceTOMLStringState(state, lines[i])
			continue
		}
		nextState := advanceTOMLStringState(tomlOutside, lines[i])
		if nextState != tomlOutside {
			state = nextState
			continue
		}
		switch {
		case isTOMLKeyAssignment(lines[i], "kind"):
			kindLine = i
		case isTOMLKeyAssignment(lines[i], "base_url"):
			baseURLLine = i
		}
		state = nextState
	}
	if kindLine < 0 || baseURLLine < 0 {
		return fmt.Errorf("upgrade DeepSeek protocol: provider table is missing kind or base_url")
	}
	lines[kindLine] = replaceTOMLStringAssignment(lines[kindLine], kind)
	lines[baseURLLine] = replaceTOMLStringAssignment(lines[baseURLLine], baseURL)
	return nil
}

func replaceTOMLStringAssignment(line, value string) string {
	return replaceTOMLScalarAssignment(line, strconv.Quote(value))
}

func replaceTOMLScalarAssignment(line, encoded string) string {
	carriageReturn := strings.HasSuffix(line, "\r")
	line = strings.TrimSuffix(line, "\r")
	equals, err := findTOMLAssignmentEquals(line, 0, len(line))
	if err != nil {
		equals = strings.IndexByte(line, '=')
	}
	if equals < 0 {
		return line
	}
	rhs := line[equals+1:]
	leadingLen := len(rhs) - len(strings.TrimLeft(rhs, " \t"))
	leading := rhs[:leadingLen]
	suffix := ""
	if comment := tomlInlineCommentIndex(rhs); comment >= 0 {
		spaceStart := comment
		for spaceStart > 0 && (rhs[spaceStart-1] == ' ' || rhs[spaceStart-1] == '\t') {
			spaceStart--
		}
		suffix = rhs[spaceStart:]
	}
	next := line[:equals+1] + leading + encoded + suffix
	if carriageReturn {
		next += "\r"
	}
	return next
}

func tomlInlineCommentIndex(value string) int {
	inBasic, inLiteral, escaped := false, false, false
	for i := range len(value) {
		ch := value[i]
		if inBasic {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inBasic = false
			}
			continue
		}
		if inLiteral {
			if ch == '\'' {
				inLiteral = false
			}
			continue
		}
		switch ch {
		case '"':
			inBasic = true
		case '\'':
			inLiteral = true
		case '#':
			return i
		}
	}
	return -1
}
