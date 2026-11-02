package permission

import (
	"encoding/json"
	"testing"
)

// A runtime's version probe is a read; the flag that traces imports while
// running a script is not, and neither is any probe followed by more words.
func TestRuntimeVersionProbeIsReadOnlyButRunningAScriptIsNot(t *testing.T) {
	for command, want := range map[string]bool{
		"python3 -V":                   true,
		"python --version":             true,
		"node -v":                      true,
		"python3 -v payload.py":        false,
		"python -v -m http.server":     false,
		"python3 -v":                   false,
		"python3 payload.py":           false,
		"python3 -c 'print(1)'":        false,
		"node -v payload.js":           false,
		"python3 --version payload.py": false,
	} {
		args, _ := json.Marshal(map[string]string{"command": command})
		if got := BashCommandIsReadOnly(args); got != want {
			t.Errorf("%q: read-only = %v, want %v", command, got, want)
		}
	}
}
