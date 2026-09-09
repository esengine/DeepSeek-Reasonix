package doctor

import "strings"

// ResolveSessionRef turns a branch id or transcript path into the session's
// .jsonl path using the same lookup as the support bundle.
func ResolveSessionRef(ref string) (string, error) {
	return resolveSessionBundlePath(strings.TrimSpace(ref))
}
