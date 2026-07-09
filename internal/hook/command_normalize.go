package hook

import (
	"strings"

	"reasonix/internal/shellparse"
)

// NormalizeCommand repairs narrow classes of copied JSON-escaped hook commands.
// It is intentionally conservative: hook commands can be security controls, so
// only commands that were already malformed and match a known hook shape are
// rewritten.
func NormalizeCommand(command string) string {
	if fixed, ok := normalizeStaticNodeEval(command); ok {
		return fixed
	}
	if fixed, ok := normalizeEscapedNodeEval(command); ok {
		return fixed
	}
	if fixed, ok := normalizeEscapedPowerShellFile(command); ok {
		return fixed
	}
	return command
}

func normalizeStaticNodeEval(command string) (string, bool) {
	node, flag, script, ok := repairableNodeEvalArgs(command)
	if !ok {
		return "", false
	}
	return renderNodeEvalCommand(node, flag, script), true
}

func directNodeEvalArgs(command string) (string, string, string, bool) {
	fields, malformed := shellparse.StaticFields(command)
	if malformed == "" && len(fields) == 3 && isNodeCommand(fields[0]) && isNodeEvalFlag(fields[1]) {
		script, ok := repairQuotedNodeEvalScript(fields[2])
		if ok {
			return fields[0], fields[1], script, true
		}
		if isHookStdinNodeEval(fields[2]) {
			return fields[0], fields[1], fields[2], true
		}
	}
	node, flag, script, ok := escapedNodeEvalArgs(command)
	return node, flag, script, ok
}

func repairableNodeEvalArgs(command string) (string, string, string, bool) {
	fields, malformed := shellparse.StaticFields(command)
	if malformed == "" && len(fields) == 3 && isNodeCommand(fields[0]) && isNodeEvalFlag(fields[1]) {
		script, ok := repairQuotedNodeEvalScript(fields[2])
		if ok {
			return fields[0], fields[1], script, true
		}
	}
	return escapedNodeEvalArgs(command)
}

func normalizeEscapedPowerShellFile(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	_, spans, ok := repairablePowerShellFileParse(trimmed)
	if !ok || len(spans) == 0 {
		return "", false
	}
	// Replace only the escaped-quote sequences found during parsing, so
	// well-formed sibling arguments keep their bytes verbatim.
	var b strings.Builder
	prev := 0
	for _, span := range spans {
		b.WriteString(trimmed[prev:span[0]])
		b.WriteByte('"')
		prev = span[1]
	}
	b.WriteString(trimmed[prev:])
	return b.String(), true
}

func repairablePowerShellFileArgs(command string) (string, []string, bool) {
	fields, _, ok := repairablePowerShellFileParse(command)
	if !ok {
		return "", nil, false
	}
	return fields[0], fields[1:], true
}

func repairablePowerShellFileParse(command string) ([]string, [][2]int, bool) {
	fields, repaired, spans, ok := parseSimpleHookCommandFields(command)
	if !ok || len(fields) < 3 || !isPowerShellCommand(fields[0]) {
		return nil, nil, false
	}
	fileIdx := powerShellFileFlagIndex(fields)
	if fileIdx < 0 || fileIdx+1 >= len(fields) {
		return nil, nil, false
	}
	if !powerShellFileRepairApplies(repaired, fileIdx) {
		return nil, nil, false
	}
	return fields, spans, true
}

func powerShellFileRepairApplies(repaired []bool, fileIdx int) bool {
	for i, ok := range repaired {
		if !ok {
			continue
		}
		if i == 0 || i > fileIdx {
			return true
		}
	}
	return false
}

func powerShellFileFlagIndex(fields []string) int {
	for i := 1; i < len(fields); i++ {
		if strings.EqualFold(fields[i], "-File") {
			return i
		}
	}
	return -1
}

func parseSimpleHookCommandFields(command string) ([]string, []bool, [][2]int, bool) {
	// A newline is a shell command separator, not argument whitespace; leave
	// multi-command strings alone like other compound commands.
	if strings.ContainsAny(command, "\n\r") {
		return nil, nil, nil, false
	}
	s := strings.TrimSpace(command)
	fields := []string{}
	repaired := []bool{}
	spans := [][2]int{}
	for i := 0; i < len(s); {
		for i < len(s) && isShellWhitespace(s[i]) {
			i++
		}
		if i >= len(s) {
			break
		}
		var b strings.Builder
		fieldStarted := false
		fieldRepaired := false
		var quote byte
		escapedQuote := false
		for i < len(s) {
			c := s[i]
			if quote == 0 {
				if isShellWhitespace(c) {
					break
				}
				if isShellControl(c) {
					return nil, nil, nil, false
				}
				if n := escapedShellQuoteLen(s, i); n > 0 {
					quote = '"'
					escapedQuote = true
					fieldStarted = true
					fieldRepaired = true
					spans = append(spans, [2]int{i, i + n})
					i += n
					continue
				}
				if c == '"' || c == '\'' {
					quote = c
					escapedQuote = false
					fieldStarted = true
					i++
					continue
				}
				b.WriteByte(c)
				fieldStarted = true
				i++
				continue
			}
			if escapedQuote {
				if n := escapedShellQuoteLen(s, i); n > 0 {
					quote = 0
					escapedQuote = false
					fieldRepaired = true
					spans = append(spans, [2]int{i, i + n})
					i += n
					continue
				}
				b.WriteByte(c)
				i++
				continue
			}
			if c == quote {
				quote = 0
				i++
				continue
			}
			if quote == '"' && c == '\\' && i+1 < len(s) && isDoubleQuoteEscapedByte(s[i+1]) {
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			b.WriteByte(c)
			i++
		}
		if quote != 0 {
			return nil, nil, nil, false
		}
		if !fieldStarted {
			return nil, nil, nil, false
		}
		fields = append(fields, b.String())
		repaired = append(repaired, fieldRepaired)
		for i < len(s) && isShellWhitespace(s[i]) {
			i++
		}
	}
	return fields, repaired, spans, len(fields) > 0
}

func escapedShellQuoteLen(s string, i int) int {
	j := i
	for j < len(s) && s[j] == '\\' {
		j++
	}
	if j == i || j >= len(s) || s[j] != '"' {
		return 0
	}
	return j - i + 1
}

func isShellWhitespace(c byte) bool {
	return c == ' ' || c == '\t'
}

func isShellControl(c byte) bool {
	switch c {
	case '&', '|', ';', '<', '>':
		return true
	default:
		return false
	}
}

func isDoubleQuoteEscapedByte(c byte) bool {
	switch c {
	case '"', '\\', '$', '`', '\n':
		return true
	default:
		return false
	}
}

func normalizeEscapedNodeEval(command string) (string, bool) {
	node, flag, script, ok := escapedNodeEvalArgs(command)
	if !ok {
		return "", false
	}
	return renderNodeEvalCommand(node, flag, script), true
}

func escapedNodeEvalArgs(command string) (string, string, string, bool) {
	command = strings.TrimSpace(command)
	for _, prefix := range []struct {
		raw  string
		node string
		flag string
	}{
		{raw: `node -e `, node: "node", flag: "-e"},
		{raw: `node --eval `, node: "node", flag: "--eval"},
		{raw: `node.exe -e `, node: "node.exe", flag: "-e"},
		{raw: `node.exe --eval `, node: "node.exe", flag: "--eval"},
	} {
		rest, ok := strings.CutPrefix(command, prefix.raw)
		if !ok {
			continue
		}
		script, ok := trimEscapedQuotes(strings.TrimSpace(rest))
		if !ok || !isHookStdinNodeEval(script) {
			return "", "", "", false
		}
		return prefix.node, prefix.flag, script, true
	}
	return "", "", "", false
}

func repairQuotedNodeEvalScript(script string) (string, bool) {
	for _, trim := range []func(string) (string, bool){
		trimDoubleQuotes,
		trimEscapedQuotes,
		trimBackslashEscapedQuotes,
	} {
		if candidate, ok := trim(strings.TrimSpace(script)); ok && isHookStdinNodeEval(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func trimDoubleQuotes(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	return s[1 : len(s)-1], true
}

func trimEscapedQuotes(s string) (string, bool) {
	if len(s) < 4 || !strings.HasPrefix(s, `\"`) || !strings.HasSuffix(s, `\"`) {
		return "", false
	}
	return unescapeJSONStyleQuotes(s[2 : len(s)-2]), true
}

func trimBackslashEscapedQuotes(s string) (string, bool) {
	if len(s) < 6 || !strings.HasPrefix(s, `\\"`) || !strings.HasSuffix(s, `\\"`) {
		return "", false
	}
	return unescapeJSONStyleQuotes(s[3 : len(s)-3]), true
}

func unescapeJSONStyleQuotes(s string) string {
	return strings.ReplaceAll(s, `\"`, `"`)
}

func isHookStdinNodeEval(script string) bool {
	script = strings.TrimSpace(script)
	if script == "" {
		return false
	}
	if !startsLikeJSStatement(script) {
		return false
	}
	return strings.Contains(script, "JSON.parse") &&
		(strings.Contains(script, "readFileSync(0") || strings.Contains(script, "readFileSync( 0") || strings.Contains(script, "process.stdin"))
}

func startsLikeJSStatement(script string) bool {
	for _, prefix := range []string{
		"const ", "const\t", "let ", "let\t", "var ", "var\t",
		"import ", "require(", "if ", "if(", "try ", "(async ", "async ",
	} {
		if strings.HasPrefix(script, prefix) {
			return true
		}
	}
	return false
}

func isNodeCommand(command string) bool {
	base := strings.ToLower(shellparse.WordBase(command))
	return base == "node" || base == "node.exe"
}

func isPowerShellCommand(command string) bool {
	base := strings.ToLower(command)
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return base == "powershell" || base == "powershell.exe" || base == "pwsh" || base == "pwsh.exe"
}

func isNodeEvalFlag(flag string) bool {
	return flag == "-e" || flag == "--eval"
}

func renderNodeEvalCommand(node, flag, script string) string {
	return shellField(node) + " " + shellField(flag) + " " + shellDoubleQuote(script)
}

func shellField(s string) string {
	if s != "" {
		safe := true
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
				r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '\\' {
				continue
			}
			safe = false
			break
		}
		if safe {
			return s
		}
	}
	return shellDoubleQuote(s)
}

func shellDoubleQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"', '$', '`':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
