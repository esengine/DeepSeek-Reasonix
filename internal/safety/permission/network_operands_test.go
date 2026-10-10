package permission

import (
	"encoding/json"
	"testing"

	"reasonix/internal/base/fileutil"
)

func holdWindowsShellPaths(t *testing.T, on bool) {
	t.Helper()
	prev := fileutil.HostIsWindows
	fileutil.HostIsWindows = on
	t.Cleanup(func() { fileutil.HostIsWindows = prev })
}

func TestNetworkPathShellCommandsAskInDefaultMode(t *testing.T) {
	holdWindowsShellPaths(t, true)
	policy := New("ask", nil, nil, nil)
	cases := []struct {
		command string
		want    Decision
	}{
		{`type \\evil\x`, Ask},
		{`cat //evil/x`, Ask},
		{`ls //evil/x`, Ask},
		{`head -n 3 //evil/x/y`, Ask},
		{`find //evil/x -name a`, Ask},
		{`grep -r x //evil/x`, Ask},
		{`Get-Content \\evil\x`, Ask},
		{`cat a.txt | head //evil/x`, Ask},
		{`cat //evil/x | head`, Ask},
		{`ls && cat //evil/x`, Ask},
		{`(cat //evil/x)`, Ask},
		{`{ cat //evil/x; }`, Ask},
		{`for f in a; do cat //evil/x; done`, Ask},
		{`cat "$(cat //evil/x)"`, Ask},
		{`cat //evil/x > out.txt`, Ask},
		{`cat < //evil/x`, Ask},
		{`env cat //evil/x`, Ask},
		{`command cat //evil/x`, Ask},
		{`xargs cat //evil/x`, Ask},
		{`time cat //evil/x`, Ask},
		{`\\evil\share\a.exe -v`, Ask},
		{`grep -f//h/s/x pat`, Ask},
		{`rg -f//h/s/x pat`, Ask},
		{`Get-Content FileSystem::\\h\s\x`, Ask},
		{`Get-ChildItem FileSystem::\\h\s`, Ask},
		{`Get-Content -Path:FileSystem::\\h\s\x`, Ask},
		{`cat /\h\s/x`, Ask},
		{`cat \/h/s/x`, Ask},
		{`cat {//h/s/x,y}`, Ask},
		{`cat "$(echo /)/h/s/x"`, Ask},
		{`Get-Content a.txt,//h/s/x`, Ask},
		{`Select-String -Path a,//h/s/x pat`, Ask},
		{`Get-Content -Path:a,\\h\s\x`, Ask},
		{`echo a,b`, Allow},
		{`cut -d, -f1 a.csv`, Allow},
		{`git log --format=%h,%s`, Allow},
		{`grep "\bfoo\b" a.go`, Allow},
		{`rg '\d+' src`, Allow},
		{`printf '\n'`, Allow},
		{`find . -name '\*.go'`, Allow},
		{"cat <<EOF\n//evil/x\nEOF", Ask},
		{`cat a.txt`, Allow},
		{`cat a.txt | head -n 3`, Allow},
		{`ls -la src && grep -r x src`, Allow},
		{`Get-Content a.txt`, Allow},
		{`cat C:\x\y`, Allow},
	}
	for _, tc := range cases {
		args, _ := json.Marshal(map[string]string{"command": tc.command})
		ro := BashCommandIsReadOnly(args)
		got := policy.Decide("bash", ro, args)
		if got != tc.want {
			t.Errorf("Decide(bash %q) = %v (read-only %v), want %v", tc.command, got, ro, tc.want)
		}
	}
}

func TestNetworkPathShellCommandsUnchangedOffWindows(t *testing.T) {
	holdWindowsShellPaths(t, false)
	args, _ := json.Marshal(map[string]string{"command": "cat //evil/x"})
	if !BashCommandIsReadOnly(args) {
		t.Error("`cat //evil/x` stopped being read-only off Windows")
	}
}

func TestNetworkPathBypassSpellingsAreNotReadOnly(t *testing.T) {
	holdWindowsShellPaths(t, true)
	for _, command := range []string{`grep -f//h/s/x p`, `Get-Content FileSystem::\\h\s\x`, `cat /\h\s/x`, `cat {//h/s/x,y}`, `cat "$(echo /)/h/s/x"`, `Get-Content a.txt,//h/s/x`, `Select-String -Path a,//h/s/x p`} {
		args, _ := json.Marshal(map[string]string{"command": command})
		if BashCommandIsReadOnly(args) {
			t.Errorf("BashCommandIsReadOnly(%q) = true; a read-only sub-agent would run it without approval", command)
		}
	}
}

func TestNetworkPathDefeatsRememberedPrefixRule(t *testing.T) {
	holdWindowsShellPaths(t, true)
	policy := New("ask", []string{"Bash(type *)", "Bash(cat *)"}, nil, nil)
	for command, want := range map[string]Decision{
		`type \\evil\x`:  Ask,
		`cat //evil/x`:   Ask,
		`type notes.txt`: Allow,
	} {
		args, _ := json.Marshal(map[string]string{"command": command})
		if got := policy.Decide("bash", BashCommandIsReadOnly(args), args); got != want {
			t.Errorf("Decide(%q) with a remembered prefix rule = %v, want %v", command, got, want)
		}
	}
	dontAsk := New("deny", []string{"Bash(type *)"}, nil, nil)
	args, _ := json.Marshal(map[string]string{"command": `type \\evil\x`})
	if got := dontAsk.Decide("bash", false, args); got != Deny {
		t.Errorf("unattended mode decided %v for a network path, want Deny", got)
	}
}
