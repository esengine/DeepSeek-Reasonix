package sandbox

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const denialLifetime = 10 * time.Minute

type denialRecord struct {
	commandHash [32]byte
	preset      string
	expires     time.Time
}

var sandboxDenials = struct {
	sync.Mutex
	records map[string]denialRecord
}{records: make(map[string]denialRecord)}

// IssueDenial records a concrete sandbox failure and returns an opaque,
// short-lived single-use identifier. The identifier is safe to show in tool
// output; it carries no path or command content.
func IssueDenial(command, preset string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return ""
	}
	id := hex.EncodeToString(random)
	now := time.Now()
	sandboxDenials.Lock()
	for key, record := range sandboxDenials.records {
		if now.After(record.expires) {
			delete(sandboxDenials.records, key)
		}
	}
	sandboxDenials.records[id] = denialRecord{
		commandHash: sha256.Sum256([]byte(command)),
		preset:      strings.TrimSpace(preset),
		expires:     now.Add(denialLifetime),
	}
	sandboxDenials.Unlock()
	return id
}

// ConsumeDenial validates and consumes a denial for an exact command. A token
// cannot be replayed for a different command or after it expires.
func ConsumeDenial(id, command string) bool {
	id = strings.TrimSpace(id)
	command = strings.TrimSpace(command)
	if id == "" || command == "" {
		return false
	}
	now := time.Now()
	sandboxDenials.Lock()
	defer sandboxDenials.Unlock()
	record, ok := sandboxDenials.records[id]
	delete(sandboxDenials.records, id)
	return ok && now.Before(record.expires) && record.commandHash == sha256.Sum256([]byte(command)) && record.preset != "danger-full-access"
}
