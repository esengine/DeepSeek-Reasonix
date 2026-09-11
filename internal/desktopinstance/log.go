//go:build windows

package desktopinstance

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Diagnostics stay on this computer and contain no business configuration.
// Each recovery log and its single backup are bounded to approximately 1 MiB.
func attemptLog(home, action, root string) func(error) {
	id := fmt.Sprintf("%x", randomID())
	writeRecoveryLog(home, fmt.Sprintf("attempt=%s action=%s root=%q phase=begin", id, action, root))
	return func(err error) {
		result := "completed"
		if err != nil {
			result = err.Error()
		}
		writeRecoveryLog(home, fmt.Sprintf("attempt=%s action=%s result=%q", id, action, result))
	}
}

func randomID() []byte { b := make([]byte, 16); _, _ = rand.Read(b); return b }

func writeRecoveryLog(home, message string) {
	path := filepath.Join(home, "desktop-shell", "logs", "recovery.log")
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Size() >= 1024*1024 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	if len(message) > 16*1024 {
		message = message[:16*1024]
	}
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), message)
}
