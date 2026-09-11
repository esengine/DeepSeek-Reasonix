package config

import (
	"fmt"
	"strconv"
	"strings"
)

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
		case kind == "openai" && (isTOMLKeyAssignment(lines[i], "request_url") || isTOMLKeyAssignment(lines[i], "chat_url")):
			// Only reached for an eligible official endpoint. Clear the standard
			// override so the derived endpoint applies and independent search
			// stays enabled.
			_, value, _ := tomlKeyValue(lines[i])
			if value != `""` && value != `''` {
				lines[i] = replaceTOMLStringAssignment(lines[i], "")
			}
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
