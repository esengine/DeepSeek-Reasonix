package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestPythonServerNameStopsAtInterpreterEntryPoint(t *testing.T) {
	cases := []struct {
		args []string
		name string
	}{
		{[]string{"reasonix_script.py", "-m", "client-mode"}, "reasonix-script-py"},
		{[]string{"--", "reasonix_script.py", "-m", "client-mode"}, "reasonix-script-py"},
		{[]string{"-I", "reasonix_script.py", "-m", "client-mode"}, "reasonix-script-py"},
		{[]string{"-W", "ignore", "reasonix_script.py", "-m", "client-mode"}, "reasonix-script-py"},
		{[]string{"-X", "dev", "reasonix_script.py", "-m", "client-mode"}, "reasonix-script-py"},
		{[]string{"--check-hash-based-pycs", "default", "reasonix_script.py", "-m", "client-mode"}, "reasonix-script-py"},
		{[]string{"-m", "reasonix_probe", "-m", "client-mode"}, "reasonix-probe"},
		{[]string{"-W", "ignore", "-X", "dev", "-m", "reasonix_probe", "-m", "client-mode"}, "reasonix-probe"},
		{[]string{"-Wignore", "-Xdev", "-mreasonix_probe", "-m", "client-mode"}, "reasonix-probe"},
		{[]string{"-c", "print(1)", "-m", "client-mode"}, "mcp-server"},
		{[]string{"-cprint(1)", "-m", "client-mode"}, "mcp-server"},
		{[]string{"-", "-m", "client-mode"}, "mcp-server"},
		{[]string{"-m"}, "mcp-server"},
		{nil, "mcp-server"},
	}
	for _, command := range []string{"python", "python3", "py"} {
		for _, tc := range cases {
			t.Run(command+" "+strings.Join(tc.args, " "), func(t *testing.T) {
				if got := NameFromArgv(command, tc.args); got != tc.name {
					t.Errorf("name = %q, want %q", got, tc.name)
				}
				if len(tc.args) == 0 {
					return
				}
				e, err := ParseArgs(append([]string{"--", command}, tc.args...))
				if err != nil {
					t.Fatal(err)
				}
				if e.Name != tc.name || e.Command != command || !reflect.DeepEqual(e.Args, tc.args) {
					t.Errorf("CLI entry = %+v, want name %q and original argv", e, tc.name)
				}
				explicit, err := ParseArgs(append([]string{"manual", "--", command}, tc.args...))
				if err != nil || explicit.Name != "manual" || explicit.Command != command || !reflect.DeepEqual(explicit.Args, tc.args) {
					t.Errorf("explicit name = %+v, %v", explicit, err)
				}
			})
		}
	}
	if got := NameFromArgv("node", []string{"server.js", "-m", "client-mode"}); got != "server" {
		t.Errorf("node script control = %q", got)
	}
}
