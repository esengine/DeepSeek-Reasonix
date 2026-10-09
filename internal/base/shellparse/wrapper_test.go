package shellparse

import (
	"reflect"
	"testing"
)

func TestPeelWrappers(t *testing.T) {
	cases := []struct {
		name   string
		cmd    string
		layers [][]string
		opaque bool
		ok     bool
	}{
		{"plain", "rm -rf b", [][]string{{"rm", "-rf", "b"}}, false, true},
		{"env assignment", "env X=1 rm -rf b", [][]string{{"env", "X=1", "rm", "-rf", "b"}, {"rm", "-rf", "b"}}, false, true},
		{"env dynamic assignment value", "env X=$HOME rm b", [][]string{{"env"}, {"rm", "b"}}, false, true},
		{"env flags", "env -i -u FOO -C /tmp rm b", [][]string{{"env", "-i", "-u", "FOO", "-C", "/tmp", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env attached arg", "env -uFOO --unset=BAR rm b", [][]string{{"env", "-uFOO", "--unset=BAR", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env double dash", "env -- rm b", [][]string{{"env", "--", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env split string is opaque", "env -S 'rm b'", [][]string{{"env", "-S", "rm b"}}, true, true},
		{"env dynamic program", "env X=1 $CMD b", [][]string{{"env", "X=1"}}, true, true},
		{"env double-quoted assignment", `env "X=1" rm b`, [][]string{{"env", "X=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env single-quoted assignment", `env 'X=1' rm b`, [][]string{{"env", "X=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env quoted name", `env "X"=1 rm b`, [][]string{{"env", "X=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env mixed quoting", `env "X"='1' rm b`, [][]string{{"env", "X=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env quoted name dynamic value", `env "X"=$HOME rm b`, [][]string{{"env"}, {"rm", "b"}}, false, true},
		{"env expansion inside quotes is not static", `env "X=$HOME" rm b`, [][]string{{"env"}}, true, true},
		{"sudo single-quoted assignment", `sudo 'X=1' rm b`, [][]string{{"sudo", "X=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"sudo double-quoted assignment", `sudo "X=1" rm b`, [][]string{{"sudo", "X=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env long option missing its value", "env --unset", [][]string{{"env", "--unset"}}, true, true},
		{"env non-identifier name", "env a-b=1 rm b", [][]string{{"env", "a-b=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env digit-leading name", "env 1X=1 rm b", [][]string{{"env", "1X=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env dotted name", "env foo.bar=1 rm b", [][]string{{"env", "foo.bar=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env quoted name with space", `env "A B=1" rm b`, [][]string{{"env", "A B=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env leading equals is opaque", "env =1 rm b", [][]string{{"env", "=1", "rm", "b"}}, true, true},
		{"env dynamic leading equals is opaque", "env =$X rm b", [][]string{{"env"}}, true, true},
		{"env dynamic option with equals is not an assignment", "env --chdir=$D rm b", [][]string{{"env"}}, true, true},
		{"sudo non-identifier name is opaque", "sudo a-b=1 rm b", [][]string{{"sudo", "a-b=1", "rm", "b"}}, true, true},
		{"sudo digit-leading name is opaque", "sudo 1X=1 rm b", [][]string{{"sudo", "1X=1", "rm", "b"}}, true, true},
		{"sudo quoted name with space is opaque", `sudo "A B=1" rm b`, [][]string{{"sudo", "A B=1", "rm", "b"}}, true, true},
		{"sudo long option with equals", "sudo --user=root rm b", [][]string{{"sudo", "--user=root", "rm", "b"}, {"rm", "b"}}, false, true},
		{"env with nothing to run", "env X=1", [][]string{{"env", "X=1"}}, false, true},
		{"sudo", "sudo rm b", [][]string{{"sudo", "rm", "b"}, {"rm", "b"}}, false, true},
		{"sudo bundled flags and user", "sudo -nE -u root rm b", [][]string{{"sudo", "-nE", "-u", "root", "rm", "b"}, {"rm", "b"}}, false, true},
		{"sudo dynamic user", "sudo -u $U rm b", [][]string{{"sudo", "-u"}, {"rm", "b"}}, false, true},
		{"sudo shell flag is opaque", "sudo -s rm b", [][]string{{"sudo", "-s", "rm", "b"}}, true, true},
		{"sudo unknown flag is opaque", "sudo --frob rm b", [][]string{{"sudo", "--frob", "rm", "b"}}, true, true},
		{"sudo option missing its value", "sudo -u", [][]string{{"sudo", "-u"}}, true, true},
		{"sudo env assignment", "sudo FOO=1 rm b", [][]string{{"sudo", "FOO=1", "rm", "b"}, {"rm", "b"}}, false, true},
		{"doas", "doas -u root rm b", [][]string{{"doas", "-u", "root", "rm", "b"}, {"rm", "b"}}, false, true},
		{"doas shell is opaque", "doas -s", [][]string{{"doas", "-s"}}, true, true},
		{"command", "command rm b", [][]string{{"command", "rm", "b"}, {"rm", "b"}}, false, true},
		{"command -p", "command -p rm b", [][]string{{"command", "-p", "rm", "b"}, {"rm", "b"}}, false, true},
		{"command -v runs nothing", "command -v rm", [][]string{{"command", "-v", "rm"}}, false, true},
		{"builtin", "builtin exec rm b", [][]string{{"builtin", "exec", "rm", "b"}, {"exec", "rm", "b"}, {"rm", "b"}}, false, true},
		{"exec flags", "exec -c -a name rm b", [][]string{{"exec", "-c", "-a", "name", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nohup", "nohup rm b", [][]string{{"nohup", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nohup unknown flag is opaque", "nohup --frob rm b", [][]string{{"nohup", "--frob", "rm", "b"}}, true, true},
		{"time keyword", "time rm b", [][]string{{"rm", "b"}}, false, true},
		{"time keyword posix", "time -p rm b", [][]string{{"rm", "b"}}, false, true},
		{"time program", "/usr/bin/time -p -o out rm b", [][]string{{"time", "-p", "-o", "out", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nice", "nice rm b", [][]string{{"nice", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nice adjustment", "nice -n 5 rm b", [][]string{{"nice", "-n", "5", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nice numeric", "nice -5 rm b", [][]string{{"nice", "-5", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nice long", "nice --adjustment=5 rm b", [][]string{{"nice", "--adjustment=5", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nice dynamic program", "nice $CMD", [][]string{{"nice"}}, true, true},
		{"negation", "! rm b", [][]string{{"rm", "b"}}, false, true},
		{"background", "nohup rm b &", [][]string{{"nohup", "rm", "b"}, {"rm", "b"}}, false, true},
		{"nested wrappers", "env sudo nohup rm b", [][]string{{"env", "sudo", "nohup", "rm", "b"}, {"sudo", "nohup", "rm", "b"}, {"nohup", "rm", "b"}, {"rm", "b"}}, false, true},
		{"unix path", "/bin/rm b", [][]string{{"rm", "b"}}, false, true},
		{"relative path", "./rm b", [][]string{{"rm", "b"}}, false, true},
		{"quoted unix path", "'/bin/rm' b", [][]string{{"rm", "b"}}, false, true},
		{"windows path forward slashes", "C:/tools/rm.exe b", [][]string{{"rm", "b"}}, false, true},
		{"windows path quoted backslashes", `'C:\tools\RM.EXE' b`, [][]string{{"rm", "b"}}, false, true},
		{"exe suffix without path", "rm.exe b", [][]string{{"rm", "b"}}, false, true},
		{"path to wrapper", "/usr/bin/env X=1 /bin/rm b", [][]string{{"env", "X=1", "/bin/rm", "b"}, {"rm", "b"}}, false, true},
		{"quoted wrapper", `"sudo" rm b`, [][]string{{"sudo", "rm", "b"}, {"rm", "b"}}, false, true},
		{"upper case name is not path", "RM b", [][]string{{"RM", "b"}}, false, true},
		{"dynamic first word is not opaque", "$CMD b", nil, false, true},
		{"compound is out of scope", "echo hi; rm b", nil, false, false},
		{"pipeline is out of scope", "ls | sudo rm b", nil, false, false},
		{"subshell is out of scope", "(rm b)", nil, false, false},
		{"group is out of scope", "{ rm b; }", nil, false, false},
		{"unparsable", "sudo 'rm", nil, false, false},
		{"empty", "", nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := PeelWrappers(tc.cmd)
			if ok != tc.ok {
				t.Fatalf("PeelWrappers(%q) ok = %v, want %v", tc.cmd, ok, tc.ok)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(got.Layers, tc.layers) && !(len(got.Layers) == 0 && len(tc.layers) == 0) {
				t.Errorf("layers = %q, want %q", got.Layers, tc.layers)
			}
			if got.Opaque != tc.opaque {
				t.Errorf("opaque = %v, want %v", got.Opaque, tc.opaque)
			}
		})
	}
}

func TestPeelWrappersDepthLimitFailsClosed(t *testing.T) {
	cmd := "rm b"
	for range 40 {
		cmd = "nohup " + cmd
	}
	got, ok := PeelWrappers(cmd)
	if !ok || !got.Opaque {
		t.Fatalf("a wrapper tower past the limit must be opaque, got ok=%v opaque=%v", ok, got.Opaque)
	}
}
