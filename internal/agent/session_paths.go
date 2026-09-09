package agent

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sessionFileComponent = regexp.MustCompile(`^[^<>:"/\\|?*\x00-\x1f\x7f]+$`)

// ContinueSessionPath keeps an existing transcript or allocates a new path.
func ContinueSessionPath(prevPath, dir, model string) string {
	if prevPath != "" {
		return prevPath
	}
	if dir == "" {
		return ""
	}
	return NewSessionPath(dir, model)
}

// NewSessionPath returns one portable filename below the session directory.
// Model labels remain hints; invalid components never become filesystem paths.
func NewSessionPath(dir, model string) string {
	safe := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "<", "-", ">", "-", "\"", "-", "|", "-", "?", "-", "*", "-").Replace(model)
	if safe == "" {
		safe = "session"
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	name := fmt.Sprintf("%s-%s.jsonl", stamp, safe)
	if !sessionFileComponent.MatchString(name) {
		return filepath.Join(dir, stamp+"-session.jsonl")
	}
	return filepath.Join(dir, name)
}
