package config

import (
	"strconv"
	"strings"
)

func isTOMLKeyAssignment(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return false
	}
	got, ok := tomlAssignmentKey(trimmed)
	return ok && got == key
}

// tomlAssignmentKey returns the simple key on a single TOML assignment line.
// It accepts bare keys and both TOML quoted-key forms while leaving dotted
// keys, malformed lines, and values containing an equals sign to the parser.
func tomlAssignmentKey(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", false
	}

	var key string
	switch line[0] {
	case '"':
		end := 1
		escaped := false
		for end < len(line) {
			if escaped {
				escaped = false
				end++
				continue
			}
			switch line[end] {
			case '\\':
				escaped = true
			case '"':
				decoded, err := strconv.Unquote(line[:end+1])
				if err != nil {
					return "", false
				}
				key = decoded
				line = line[end+1:]
				goto assignment
			}
			end++
		}
		return "", false
	case '\'':
		end := strings.IndexByte(line[1:], '\'')
		if end < 0 {
			return "", false
		}
		end++
		key = line[1:end]
		line = line[end+1:]
	default:
		end := 0
		for end < len(line) && line[end] != '=' && line[end] != ' ' && line[end] != '\t' {
			end++
		}
		if end == 0 {
			return "", false
		}
		key = line[:end]
		line = line[end:]
	}

assignment:
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "=") || strings.Contains(key, ".") {
		return "", false
	}
	return key, true
}
