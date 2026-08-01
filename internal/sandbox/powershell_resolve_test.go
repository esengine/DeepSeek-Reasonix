package sandbox

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestResolvePowerShellDecisionTable(t *testing.T) {
	onPath := func(entries map[string]string) func(string) (string, error) {
		return func(name string) (string, error) {
			if p, ok := entries[name]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		}
	}
	noneOnPath := onPath(nil)
	always := func(string) bool { return true }
	never := func(string) bool { return false }
	versioned := `C:\Program Files\PowerShell\7\pwsh.exe`
	systemPS := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	storeStub := `C:\Users\me\AppData\Local\Microsoft\WindowsApps\pwsh.exe`

	probe7 := func(string) int { return 7 }
	probe5 := func(string) int { return 5 }
	probeFails := func(string) int { return 0 }
	probeMustNotRun := func(string) int {
		t.Error("versioned pwsh 7 install must be accepted without a probe subprocess")
		return 0
	}
	probeByPath := func(table map[string]int) func(string) int {
		return func(p string) int { return table[p] }
	}

	cases := []struct {
		name       string
		prefer     string
		path       string
		lookPath   func(string) (string, error)
		exists     func(string) bool
		candidates []string
		probe      func(string) int
		wantShell  Shell
		wantWarn   string // substring; empty = no warning expected
	}{
		{
			name:      "explicit path wins",
			path:      `C:\custom\ps.exe`,
			lookPath:  noneOnPath,
			exists:    always,
			probe:     probe7,
			wantShell: Shell{Kind: ShellPowerShell, Path: `C:\custom\ps.exe`, MajorVersion: 7},
		},
		{
			name:      "explicit versioned path needs no probe",
			path:      versioned,
			lookPath:  noneOnPath,
			exists:    always,
			probe:     probeMustNotRun,
			wantShell: Shell{Kind: ShellPowerShell, Path: versioned, MajorVersion: 7},
		},
		{
			name:      "missing explicit path warns and discovers",
			path:      `C:\nope\ps.exe`,
			lookPath:  onPath(map[string]string{"pwsh": `C:\real\pwsh.exe`}),
			exists:    never,
			probe:     probe7,
			wantShell: Shell{Kind: ShellPowerShell, Path: `C:\real\pwsh.exe`, MajorVersion: 7},
			wantWarn:  "does not exist",
		},
		{
			name:       "versioned install beats PATH without probing",
			lookPath:   onPath(map[string]string{"pwsh": `C:\real\pwsh.exe`}),
			exists:     always,
			candidates: []string{versioned},
			probe:      probeMustNotRun,
			wantShell:  Shell{Kind: ShellPowerShell, Path: versioned, MajorVersion: 7},
		},
		{
			name:      "WindowsApps stub skipped for real PATH hit",
			lookPath:  onPath(map[string]string{"pwsh": storeStub, "powershell": systemPS}),
			exists:    never,
			probe:     probeByPath(map[string]int{systemPS: 5}),
			wantShell: Shell{Kind: ShellPowerShell, Path: systemPS, MajorVersion: 5},
		},
		{
			name:       "prefer=powershell reverses order",
			prefer:     "powershell",
			lookPath:   noneOnPath,
			exists:     always,
			candidates: []string{versioned, systemPS},
			probe:      probeByPath(map[string]int{systemPS: 5}),
			wantShell:  Shell{Kind: ShellPowerShell, Path: systemPS, MajorVersion: 5},
		},
		{
			name:       "probe failure rejects candidate",
			lookPath:   noneOnPath,
			exists:     always,
			candidates: []string{systemPS},
			probe:      probeFails,
			wantShell:  Shell{},
			wantWarn:   "no PowerShell interpreter found",
		},
		{
			name:      "pwsh from PATH probed for its version",
			lookPath:  onPath(map[string]string{"pwsh": `C:\real\pwsh.exe`}),
			exists:    never,
			probe:     probe7,
			wantShell: Shell{Kind: ShellPowerShell, Path: `C:\real\pwsh.exe`, MajorVersion: 7},
		},
		{
			name:      "renamed pwsh reports probed version",
			lookPath:  onPath(map[string]string{"pwsh": `C:\real\renamed.exe`}),
			exists:    never,
			probe:     probe5,
			wantShell: Shell{Kind: ShellPowerShell, Path: `C:\real\renamed.exe`, MajorVersion: 5},
		},
		{
			name:      "nothing found returns zero Shell with guidance",
			lookPath:  noneOnPath,
			exists:    never,
			probe:     probeFails,
			wantShell: Shell{},
			wantWarn:  "install PowerShell 7",
		},
		{
			name:      "unrecognised prefer warns and uses default order",
			prefer:    "zsh",
			lookPath:  onPath(map[string]string{"pwsh": `C:\real\pwsh.exe`}),
			exists:    never,
			probe:     probe7,
			wantShell: Shell{Kind: ShellPowerShell, Path: `C:\real\pwsh.exe`, MajorVersion: 7},
			wantWarn:  "not recognised",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var warn bytes.Buffer
			got := resolvePowerShell(tc.prefer, tc.path, &warn, tc.lookPath, tc.exists, tc.candidates, tc.probe)
			if got != tc.wantShell {
				t.Errorf("resolvePowerShell() = %+v, want %+v", got, tc.wantShell)
			}
			if tc.wantWarn == "" && warn.Len() != 0 {
				t.Errorf("unexpected warning: %q", warn.String())
			}
			if tc.wantWarn != "" && !strings.Contains(warn.String(), tc.wantWarn) {
				t.Errorf("warning %q missing %q", warn.String(), tc.wantWarn)
			}
		})
	}
}

func TestIsWindowsAppsStub(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`C:\Users\me\AppData\Local\Microsoft\WindowsApps\pwsh.exe`, true},
		{`C:\Users\me\AppData\Local\Microsoft\windowsapps\powershell.exe`, true}, // case-insensitive
		{`C:/Users/me/AppData/Local/Microsoft/WindowsApps/pwsh.exe`, true},       // forward slashes
		{`C:\Program Files\PowerShell\7\pwsh.exe`, false},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, false},
		{`C:\Users\me\Apps\pwsh.exe`, false}, // substring, not a path segment
		{"", false},
	}
	for _, tc := range cases {
		if got := isWindowsAppsStub(tc.path); got != tc.want {
			t.Errorf("isWindowsAppsStub(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsVersionedPwsh7Install(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`C:\Program Files\PowerShell\7\pwsh.exe`, true},
		{`C:/Program Files/PowerShell/7/pwsh.exe`, true},
		{`C:\program files\powershell\7\PWSH.EXE`, true},
		{`C:\Program Files\PowerShell\7-preview\pwsh.exe`, false},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, false},
		{`pwsh.exe`, false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isVersionedPwsh7Install(tc.path); got != tc.want {
			t.Errorf("isVersionedPwsh7Install(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestSupportsChainingWithMajorVersion(t *testing.T) {
	cases := []struct {
		name  string
		shell Shell
		want  bool
	}{
		{"probed 7 wins over filename", Shell{Kind: ShellPowerShell, Path: `C:\ps\powershell.exe`, MajorVersion: 7}, true},
		{"probed 5 wins over filename", Shell{Kind: ShellPowerShell, Path: `C:\ps\pwsh.exe`, MajorVersion: 5}, false},
		{"probed 5.1 as 5", Shell{Kind: ShellPowerShell, Path: "powershell", MajorVersion: 5}, false},
		{"unknown falls back to pwsh name", Shell{Kind: ShellPowerShell, Path: "pwsh"}, true},
		{"unknown falls back to powershell name", Shell{Kind: ShellPowerShell, Path: "powershell"}, false},
		{"bash always chains", Shell{Kind: ShellBash, Path: "bash"}, true},
	}
	for _, tc := range cases {
		if got := tc.shell.SupportsChaining(); got != tc.want {
			t.Errorf("%s: SupportsChaining() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolvePowerShellMemoized(t *testing.T) {
	prefer, path := "__memo_test__", `C:\memo\ps.exe`
	key := prefer + "\x00" + path
	pwshResolveMu.Lock()
	delete(pwshResolveCache, key)
	pwshResolveMu.Unlock()

	// The explicit path doesn't exist, so resolution lands on discovery; the
	// exact result is host-dependent — what matters is that the second call is
	// served from the cache (identical result, entry present).
	first := ResolvePowerShell(prefer, path, nil)
	second := ResolvePowerShell(prefer, path, nil)
	if first != second {
		t.Fatalf("memoized resolution diverged: %+v vs %+v", first, second)
	}
	pwshResolveMu.Lock()
	_, cached := pwshResolveCache[key]
	pwshResolveMu.Unlock()
	if !cached {
		t.Fatal("resolution was not memoized")
	}
}

func TestResolvePowerShellRealHost(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("real-host resolution is windows-only")
	}
	sh := ResolvePowerShell("", "", nil)
	if sh.Path == "" {
		t.Skip("no PowerShell interpreter on this host")
	}
	if sh.Kind != ShellPowerShell {
		t.Fatalf("kind = %v, want ShellPowerShell", sh.Kind)
	}
	if sh.MajorVersion < 5 {
		t.Fatalf("MajorVersion = %d, want 5 (Windows PowerShell) or 7+ (pwsh)", sh.MajorVersion)
	}
	// A real interpreter must answer the chaining question consistently with
	// its probed version.
	if got, want := sh.SupportsChaining(), sh.MajorVersion >= 7; got != want {
		t.Fatalf("SupportsChaining() = %v, want %v for major %d", got, want, sh.MajorVersion)
	}
}
