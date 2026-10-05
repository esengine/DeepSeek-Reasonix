package shellsafe

import "testing"

// The two copies of this rule had drifted: permission knew `bash -c` and the
// smaller interpreters, evidence did not, so the same command was inline code
// to the approval layer and an ordinary program to the ledger.
func TestArgvCarriesInlineCodeCoversEveryInterpreter(t *testing.T) {
	carries := [][]string{
		{"python3", "-c", "print(1)"},
		{"python", "-c", "print(1)"},
		{"node", "-e", "console.log(1)"},
		{"node", "--eval=1+1"},
		{"bun", "-p", "1"},
		{"deno", "eval", "console.log(1)"},
		{"perl", "-e", "print 1"},
		{"ruby", "-e", "puts 1"},
		{"lua", "-e", "print(1)"},
		{"rscript", "-e", "1"},
		{"osascript", "-e", "beep"},
		{"php", "-r", "echo 1;"},
		{"bash", "-c", "ls"},
		{"sh", "-lc", "ls"},
		{"zsh", "--command", "ls"},
		{"/usr/bin/python3", "-c", "print(1)"},
		{"pwsh", "-Command", "ls"},
		{"cmd", "/c", "dir"},
	}
	for _, argv := range carries {
		if !ArgvCarriesInlineCode(argv) {
			t.Errorf("%v: inline code went unrecognised", argv)
		}
	}
	plain := [][]string{
		{"python3", "script.py"},
		{"node", "index.js"},
		{"bash", "install.sh"},
		{"bash", "--", "-c"}, // after --, no more options
		{"go", "test", "./..."},
		{"grep", "-e", "pattern", "file.txt"}, // -e is not code here
		{},
	}
	for _, argv := range plain {
		if ArgvCarriesInlineCode(argv) {
			t.Errorf("%v: an ordinary call read as inline code", argv)
		}
	}
}

func TestExecutableBaseIgnoresPathAndExtension(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"python3", "python3"},
		{"/usr/bin/Python3", "python3"},
		{`C:\Windows\System32\cmd.exe`, "cmd"},
		{"", ""},
	} {
		if got := ExecutableBase(tc.in); got != tc.want {
			t.Errorf("ExecutableBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Membership and flag spellings come from one table, so a program cannot be an
// interpreter to one caller and unknown to the next.
func TestIsInlineCodeInterpreterMatchesTheFlagTable(t *testing.T) {
	for _, program := range []string{
		"python", "python3", "py", "pypy3", "node", "bun", "deno", "perl", "ruby",
		"lua", "luajit", "r", "rscript", "osascript", "php", "bash", "sh", "zsh",
		"ksh", "dash", "fish", "powershell", "pwsh", "cmd",
		"/usr/bin/python3", `C:\Python\python.exe`, "PYTHON",
	} {
		if !IsInlineCodeInterpreter(program) {
			t.Errorf("IsInlineCodeInterpreter(%q) = false, want true", program)
		}
	}
	for _, program := range []string{"go", "cat", "grep", "make", "git", "", "pythonic"} {
		if IsInlineCodeInterpreter(program) {
			t.Errorf("IsInlineCodeInterpreter(%q) = true, want false", program)
		}
	}
}

// Handed a here-doc, an interpreter with no program to run reads one from it.
// One that names a script or a module has its source in a file the host can
// read, and the body is only that program's input.
func TestArgvTakesProgramFromStdin(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"bare interpreter", []string{"python3"}, true},
		{"explicit stdin marker", []string{"python3", "-"}, true},
		{"flags only", []string{"python3", "-u"}, true},
		{"shell told to read stdin", []string{"sh", "-s"}, true},
		{"node with no operand", []string{"node"}, true},
		{"a script operand", []string{"python3", "script.py"}, false},
		{"a module operand", []string{"python3", "-m", "tool"}, false},
		{"an operand after the option terminator", []string{"python3", "--", "script.py"}, false},
		{"not an interpreter", []string{"cat"}, false},
		{"nothing at all", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ArgvTakesProgramFromStdin(tc.argv); got != tc.want {
				t.Fatalf("ArgvTakesProgramFromStdin(%q) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}
