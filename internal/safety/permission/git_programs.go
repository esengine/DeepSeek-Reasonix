package permission

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/safety/shellsafe"
)

// gitReadPrograms names the program-running keys of the repository a bash call's
// git reads would run in: the call is a read only while the repository does not
// carry its own. A call the host cannot tie to a repository is not inspected, so
// it is treated as carrying them.
func (p Policy) gitReadPrograms(ctx context.Context, args json.RawMessage) string {
	if p.RepoPrograms == nil {
		return ""
	}
	var call struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(args, &call) != nil {
		return ""
	}
	dirs, unknown := shellsafe.GitContentReadTargets(call.Command)
	if unknown {
		return "a repository the host could not inspect"
	}
	var keys []string
	for _, dir := range dirs {
		keys = append(keys, p.RepoPrograms(ctx, dir)...)
	}
	return strings.Join(uniqueStrings(keys), ", ")
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
