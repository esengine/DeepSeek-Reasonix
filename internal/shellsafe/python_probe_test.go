package shellsafe

import "testing"

// -v is verbose import tracing in python, not --version: `python3 -v script.py`
// runs the script, so it must not be read as a version probe.
func TestPythonVerboseIsNotAVersionProbe(t *testing.T) {
	for _, c := range []string{
		"python3 -v script.py", "python -v script.py", "python3 -v", "python3 -v -m http.server",
		"python3 -vv script.py", "python3 -v -c 'import os'",
	} {
		if _, _, ok := CommandIsReadOnly(c); ok {
			t.Errorf("CommandIsReadOnly(%q) = true, want false", c)
		}
		if effect := ClassifyBash(c); effect.PermissionSafe {
			t.Errorf("ClassifyBash(%q) is permission-safe, want not", c)
		}
	}
}

func TestPythonVersionProbesStayReadOnly(t *testing.T) {
	for _, c := range []string{"python --version", "python3 --version", "python3 -V", "python -V"} {
		if _, _, ok := CommandIsReadOnly(c); !ok {
			t.Errorf("CommandIsReadOnly(%q) = false, want true", c)
		}
		if effect := ClassifyBash(c); !effect.PermissionSafe {
			t.Errorf("ClassifyBash(%q) is not permission-safe, want safe", c)
		}
	}
}
