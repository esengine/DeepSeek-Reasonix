package hook

import (
	"strings"

	"reasonix/internal/shellparse"
)

// NormalizeCommand repairs a narrow class of copied JSON-escaped node -e hooks.
// It is intentionally conservative: hook commands can be security controls, so
// only a single static node -e command whose script clearly reads hook payload
// JSON from stdin is rewritten.
func NormalizeCommand(command string) string {
	if fixed, ok := normalizeStaticNodeEval(command); ok {
		return fixed
	}
	if fixed, ok := normalizeEscapedNodeEval(command); ok {
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
