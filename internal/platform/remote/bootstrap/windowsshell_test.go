package bootstrap

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"

	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/sftpfs"
)

// decodePS reads back what cmd would have handed PowerShell, so a test can
// assert on the script rather than on its wrapper.
func decodePS(t *testing.T, command string) string {
	t.Helper()
	_, encoded, ok := strings.Cut(command, "-EncodedCommand ")
	if !ok {
		t.Fatalf("command is not encoded: %s", command)
	}
	if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decodeB64UTF16(encoded)
}

func hostileWindowsPaths(t *testing.T) (StatePaths, string, string) {
	t.Helper()
	home := `C:\Users\Ada O'Hara & Co`
	workspace := `C:\work\a dir; rm -rf %USERPROFILE%`
	return windowsShell{}.Paths(home, workspace), `C:\bin\re asonix.exe`, workspace
}

// cmd receives every one of these, and cmd's escaping rules are where this
// would become a security bug. Base64 has no character it treats as special,
// so there is nothing for a path to break out of.
func TestWindowsCommandsHandCmdNothingItCanInterpret(t *testing.T) {
	paths, bin, workspace := hostileWindowsPaths(t)
	shell := windowsShell{}
	for name, command := range map[string]string{
		"launch": shell.Launch(LaunchSpec{Bin: bin, Workspace: workspace}, paths),
		"alive":  shell.Alive(4242, paths),
		"stop":   shell.Stop(4242, paths),
		"logs":   shell.Logs(paths.LogFile, 50),
		"locate": shell.Locate(`C:\up load\reasonix.exe`, LaunchFlags(true)),
	} {
		const prefix = "powershell -NoProfile -NonInteractive -EncodedCommand "
		payload, ok := strings.CutPrefix(command, prefix)
		if !ok {
			t.Fatalf("%s: not delivered encoded: %s", name, command)
		}
		if strings.ContainsAny(payload, " \t\"'&|<>^%()") {
			t.Fatalf("%s: payload is not inert to cmd: %q", name, payload)
		}
		if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
			t.Fatalf("%s: payload is not base64: %v", name, err)
		}
	}
}

// Inside the script the rule is PowerShell's: a single-quoted literal expands
// nothing, and the only escape is a doubled quote. A path carrying one must
// come back out as itself.
func TestWindowsLaunchQuotesHostilePaths(t *testing.T) {
	paths, bin, workspace := hostileWindowsPaths(t)
	outer := decodePS(t, windowsShell{}.Launch(LaunchSpec{Bin: bin, Workspace: workspace}, paths))

	// The apostrophe in the home directory reaches the script doubled, which is
	// how PowerShell spells a literal one.
	if !strings.Contains(outer, "O''Hara") {
		t.Fatalf("apostrophe was not escaped for PowerShell:\n%s", outer)
	}
	// A directory named like a command substitution is data, not a command.
	if strings.Contains(outer, "; rm -rf ") && !strings.Contains(outer, "'") {
		t.Fatal("workspace escaped its quotes")
	}
	// The inner script is itself encoded, so the serve's own arguments never
	// pass through a second round of shell parsing.
	inner := decodePS(t, "-EncodedCommand "+innerPayload(t, outer))
	if !strings.Contains(inner, "--token-file") || !strings.Contains(inner, "--port-file") {
		t.Fatalf("inner script does not start a serve:\n%s", inner)
	}
	if !strings.Contains(inner, `Users\Ada O''Hara & Co`) {
		t.Fatalf("the path did not survive into the inner script:\n%s", inner)
	}
	// %USERPROFILE% must stay text: it is part of a directory name here, and a
	// shell that expanded it would delete somewhere else entirely.
	if !strings.Contains(inner, "%USERPROFILE%") {
		t.Fatalf("a literal %%USERPROFILE%% was lost:\n%s", inner)
	}
}

// innerPayload pulls the nested -EncodedCommand argument out of the launcher.
func innerPayload(t *testing.T, outer string) string {
	t.Helper()
	const marker = "-EncodedCommand "
	at := strings.LastIndex(outer, marker)
	if at < 0 {
		t.Fatalf("no nested command in:\n%s", outer)
	}
	rest := outer[at+len(marker):]
	end := strings.IndexAny(rest, "'\" ")
	if end < 0 {
		t.Fatalf("nested command does not end:\n%s", outer)
	}
	return rest[:end]
}

// The two spellings of one path: the file layer addresses /C:/Users/..., the
// machine's own shell wants C:\Users\... Getting this backwards is a file the
// other half cannot find.
func TestWindowsPathsHaveTwoSpellings(t *testing.T) {
	for _, given := range []string{`C:\Users\ada`, `/C:/Users/ada`} {
		if got := toSFTPPath(given); got != "/C:/Users/ada" {
			t.Fatalf("toSFTPPath(%q) = %q", given, got)
		}
		if got := toShellPath(toSFTPPath(given)); got != `C:\Users\ada` {
			t.Fatalf("round trip of %q = %q", given, got)
		}
	}
}

// A machine that answered with something other than a platform is as
// unsupported as one that named the wrong OS — including the case where the
// shell did not expand the variables at all.
func TestParseWindowsEnv(t *testing.T) {
	for _, test := range []struct{ out, goos, goarch string }{
		{"Windows_NT AMD64\r\n", "windows", "amd64"},
		{"windows_nt ARM64", "windows", "arm64"},
	} {
		goos, goarch, err := ParseWindowsEnv(test.out)
		if err != nil || goos != test.goos || goarch != test.goarch {
			t.Fatalf("ParseWindowsEnv(%q) = %q/%q, %v", test.out, goos, goarch, err)
		}
	}
	for _, out := range []string{"", "%OS% %PROCESSOR_ARCHITECTURE%", "Windows_NT IA64", "Linux x86_64"} {
		if _, _, err := ParseWindowsEnv(out); err == nil {
			t.Fatalf("ParseWindowsEnv(%q) was accepted", out)
		}
	}
}

// loginShell stands in for whichever shell OpenSSH on Windows was configured to
// start. Each one runs powershell.exe the same way; they differ in what they
// make of uname and of cmd's %VAR% syntax.
type loginShell struct {
	uname   string // stdout of `uname -sm`, empty when the shell has no uname
	percent bool   // whether %VAR% expands, which only cmd does
}

func (s loginShell) Exec(_ context.Context, cmd string) (remote.ExecResult, error) {
	if strings.HasPrefix(cmd, "powershell ") {
		_, encoded, _ := strings.Cut(cmd, "-EncodedCommand ")
		if strings.ContainsAny(encoded, " \t") {
			return remote.ExecResult{ExitCode: 1}, nil
		}
		if strings.Contains(decodeB64UTF16(encoded), "$env:PROCESSOR_ARCHITECTURE") {
			return remote.ExecResult{Stdout: []byte("Windows_NT AMD64\r\n")}, nil
		}
		return remote.ExecResult{}, nil
	}
	if cmd == "uname -sm" {
		if s.uname == "" {
			return remote.ExecResult{ExitCode: 1, Stderr: []byte("'uname' is not recognized")}, nil
		}
		return remote.ExecResult{Stdout: []byte(s.uname)}, nil
	}
	if out, ok := strings.CutPrefix(cmd, "echo "); ok {
		if s.percent {
			out = strings.NewReplacer("%OS%", "Windows_NT", "%PROCESSOR_ARCHITECTURE%", "AMD64").Replace(out)
		}
		return remote.ExecResult{Stdout: []byte(out + "\n")}, nil
	}
	return remote.ExecResult{ExitCode: 127}, nil
}

func (loginShell) SFTP() (*sftpfs.FS, error) { return nil, errors.New("no file layer here") }

func decodeB64UTF16(encoded string) string {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	return string(utf16.Decode(units))
}

// OpenSSH's DefaultShell can be cmd, PowerShell or Git Bash, and the machine
// behind all three is the same Windows host the rest of windowsShell drives.
func TestWindowsIsIdentifiedWhateverTheLoginShell(t *testing.T) {
	for name, shell := range map[string]loginShell{
		"cmd":        {percent: true},
		"powershell": {},
		"git bash":   {uname: "MSYS_NT-10.0-26200 x86_64\n"},
		"mingw":      {uname: "MINGW64_NT-10.0-19045 x86_64\n"},
	} {
		goos, goarch, err := detectPlatform(context.Background(), shell)
		if err != nil || goos != "windows" || goarch != "amd64" {
			t.Fatalf("%s: detectPlatform = %q/%q, %v; want windows/amd64", name, goos, goarch, err)
		}
	}
}
