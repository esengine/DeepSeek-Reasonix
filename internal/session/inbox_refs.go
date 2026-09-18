package session

import (
	"reasonix/internal/sessioncontent"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/store"
)

func collectInboxContentRefs(sessionDir string) []sessioncontent.Ref {
	refs, _ := collectInboxContentRefsChecked(sessionDir)
	return refs
}

func collectInboxContentRefsChecked(sessionDir string) ([]sessioncontent.Ref, error) {
	return sessioninbox.FrozenContentRefs(store.SessionInboxDir(sessionDir))
}

func contentRefKey(ref sessioncontent.Ref) string {
	return ref.Digest + ":" + itoa64(ref.Bytes) + ":" + ref.IndexDigest
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
