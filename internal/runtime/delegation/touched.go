package delegation

import (
	"encoding/json"
	"strings"

	"reasonix/internal/contract/provider"
)

const maxTouchedPaths = 40

func touchedPaths(messages []provider.Message) (read, wrote []string) {
	seenRead, seenWrote := map[string]bool{}, map[string]bool{}
	for _, m := range messages {
		for _, call := range m.ToolCalls {
			var args struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(call.Arguments), &args) != nil || strings.TrimSpace(args.Path) == "" {
				continue
			}
			switch call.Name {
			case "read_file":
				read = appendOnce(read, seenRead, args.Path)
			case "write_file", "edit_file":
				wrote = appendOnce(wrote, seenWrote, args.Path)
			}
		}
	}
	return read, wrote
}

func appendOnce(list []string, seen map[string]bool, path string) []string {
	if seen[path] || len(list) >= maxTouchedPaths {
		return list
	}
	seen[path] = true
	return append(list, path)
}
