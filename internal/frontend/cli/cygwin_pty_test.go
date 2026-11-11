package cli

import (
	"os"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
)

func TestCygwinPtyNamesAreTheOnesMSYSAndCygwinGiveTheirTerminals(t *testing.T) {
	for name, want := range map[string]bool{
		`\msys-1888ae32e00d56aa-pty0-from-master`:     true,
		`\msys-1888ae32e00d56aa-pty0-to-master`:       true,
		`\cygwin-e022582115c10879-pty4-to-master`:     true,
		`\cygwin-e022582115c10879-pty4-from-master`:   true,
		`\msys-1888ae32e00d56aa-pty2-from-master-nat`: true,
		`\msys-1888ae32e00d56aa-pty2-to-master-nat`:   true,
		`\msys-1888ae32e00d56aa-pty2-to-master-nat2`:  false,
		`\msys-1888ae32e00d56aa-pty0-master`:          false,
		`\msys-1888ae32e00d56a-pty0-to-master`:        false,
		`\Device\NamedPipe\mojo.1234`:                 false,
		`\pipe\something`:                             false,
		``:                                            false,
	} {
		if got := cygwinPtyName(name); got != want {
			t.Errorf("cygwinPtyName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestAMinttyEndReplacesTheUsageTextWithTheHintAndAConsoleDoesNot(t *testing.T) {
	in, out := os.Stdin, os.Stdout
	if got := minttyHint(in, out, func(f *os.File) bool { return f == out }); got != i18n.M.MinttyConsoleHint || got == "" {
		t.Fatalf("hint = %q when stdout is a pty", got)
	}
	if got := minttyHint(in, out, func(f *os.File) bool { return f == in }); got == "" {
		t.Fatal("no hint when stdin is a pty")
	}
	if got := minttyHint(in, out, func(*os.File) bool { return false }); got != "" {
		t.Fatalf("hint %q for a real console or a pipe", got)
	}
}

func TestAskingAboutAFileOrAPipeIsAnAnswerNotAFault(t *testing.T) {
	f, err := os.CreateTemp(testenv.TempDir(t), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if onCygwinPty(f) {
		t.Fatal("a regular file reported as a Cygwin terminal")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if onCygwinPty(r) || onCygwinPty(w) {
		t.Fatal("an anonymous pipe reported as a Cygwin terminal")
	}
}
