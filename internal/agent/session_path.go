package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

var sessionPathSequence atomic.Uint64

func newSessionPathNonce() string {
	sequence := sessionPathSequence.Add(1)
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err == nil {
		return hex.EncodeToString(nonce[:])
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", SessionWriterID(), sequence)))
	return hex.EncodeToString(digest[:8])
}

// NewSessionPath returns a fresh model-namespaced session path. A nonce avoids
// concurrent Windows timestamp collisions while preserving legacy parsing.
func NewSessionPath(dir, model string) string {
	safe := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "<", "-", ">", "-", "\"", "-", "|", "-", "?", "-", "*", "-").Replace(model)
	if safe == "" {
		safe = "session"
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	return filepath.Join(dir, fmt.Sprintf("%s.%s-%s.jsonl", stamp, newSessionPathNonce(), safe))
}
