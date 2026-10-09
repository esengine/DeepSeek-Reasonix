package shellparse

import (
	"context"
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPowerShellDeleteEntryPoints(t *testing.T) {
	for _, executable := range []string{"pwsh", "powershell"} {
		t.Run(executable, func(t *testing.T) {
			path, err := exec.LookPath(executable)
			if err != nil {
				t.Skip("PowerShell executable unavailable")
			}
			cases := []struct {
				source      string
				calls       []DeleteCall
				syntaxError bool
			}{
				{`Remove-Item -Recurse '`, nil, true},
				{`if ($true) {`, nil, true},
				{``, []DeleteCall{}, false},
				{`Remove-Item -Recurse child`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", "child"}}}, false},
				{`rm -Recurse child`, []DeleteCall{{Name: "rm", Args: []string{"-Recurse", "child"}}}, false},
				{`Remove-Item`, []DeleteCall{{Name: "Remove-Item", Args: []string{}}}, false},
				{`Remove-Item -Recurse $target`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", ""}}}, false},
				{`Set-Location app; Remove-Item -Recurse child`, []DeleteCall{{Name: "Set-Location", Args: []string{"app"}}, {Name: "Remove-Item", Args: []string{"-Recurse", "child"}}}, false},
				{`if (Test-Path child) { Remove-Item -Recurse child }`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", "child"}}}, false},
				{`if ($true) { Remove-Item -Recurse child }`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", "child"}, Unsafe: true}}, false},
				{`while ($false) { Remove-Item -Recurse child }`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", "child"}, Unsafe: true}}, false},
				{`$x=1`, []DeleteCall{{Args: []string{}, Unsafe: true}}, false},
				{`function f { Remove-Item -Recurse child }`, []DeleteCall{{Args: []string{}, Unsafe: true}, {Name: "Remove-Item", Args: []string{"-Recurse", "child"}, Unsafe: true}}, false},
				{`Write-Output $(Remove-Item -Recurse child)`, []DeleteCall{{Name: "Write-Output", Args: []string{""}}, {Name: "Remove-Item", Args: []string{"-Recurse", "child"}, Unsafe: true}}, false},
				{`'child' | Remove-Item -Force`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", "child"}}}, false},
				{`Get-ChildItem child | Remove-Item -Force`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", ""}}}, false},
				{`[IO.Directory]::Delete('child',$true)`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", "child"}}}, false},
				{`[IO.Directory]::Delete('child',$false)`, []DeleteCall{}, false},
				{`[IO.Directory]::Delete('child')`, []DeleteCall{}, false},
				{`Set-Alias z Remove-Item; z -Recurse child`, []DeleteCall{{Name: "Remove-Item", Args: []string{"-Recurse", ""}, Unsafe: true}, {Name: "Remove-Item", Args: []string{"-Recurse", "child"}}}, false},
				{`iex ('Remove-Item ' + '-Recurse child')`, []DeleteCall{{Name: "iex", Args: []string{"Remove-Item -Recurse child"}}}, false},
			}
			var requests strings.Builder
			for _, tc := range cases {
				data, err := json.Marshal(tc.source)
				if err != nil {
					t.Fatal(err)
				}
				requests.Write(data)
				requests.WriteByte('\n')
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", PowerShellDeleteServer)
			cmd.Stdin = strings.NewReader(requests.String())
			output, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) != len(cases) {
				t.Fatalf("response lines = %d, want %d", len(lines), len(cases))
			}
			for i, tc := range cases {
				t.Run(tc.source, func(t *testing.T) {
					var got DeleteAnalysis
					if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
						t.Fatal(err)
					}
					if got.HostError || (got.SyntaxError != "") != tc.syntaxError || got.Standalone == tc.syntaxError || (!tc.syntaxError && !reflect.DeepEqual(got.Calls, tc.calls)) || (tc.syntaxError && len(got.Calls) != 0) {
						t.Fatalf("analysis = %#v, want calls %#v, syntax error %v", got, tc.calls, tc.syntaxError)
					}
				})
			}
			cmd = exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", PowerShellDeleteAnalysis)
			cmd.Stdin = strings.NewReader(cases[3].source)
			output, err = cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var got DeleteAnalysis
			if err := json.Unmarshal(output, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, DeleteAnalysis{Standalone: true, Calls: cases[3].calls}) {
				t.Fatalf("one-shot analysis = %#v", got)
			}
		})
	}
}
