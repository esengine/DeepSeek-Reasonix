package shellsafe

import (
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/base/shellparse"
)

func TestAnalyzeRecursiveDelete(t *testing.T) {
	tests := []struct {
		name string
		args []string
		ps   bool
		want DeleteExtent
	}{
		{"rm", nil, false, DeleteExtent{}},
		{"", nil, false, DeleteExtent{}},
		{"echo", []string{"rm", "-rf", "tree"}, false, DeleteExtent{}},
		{"rm", []string{"tree"}, false, DeleteExtent{}},
		{"rm", []string{"-rf", "tree", "other"}, false, DeleteExtent{Targets: []string{"tree", "other"}, Recursive: true}},
		{"rm", []string{"-R", "--", "-tree"}, false, DeleteExtent{Targets: []string{"-tree"}, Recursive: true}},
		{"rm", []string{"--recursive", "--force", "--verbose", "--dir", "tree"}, false, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"rm", []string{"--rec", "tree"}, false, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"rm", []string{"-rZ", "tree"}, false, DeleteExtent{Targets: []string{"tree"}, Recursive: true, UnknownOption: true}},
		{"rm", []string{"-r", "--unknown", "tree"}, false, DeleteExtent{Targets: []string{"tree"}, Recursive: true, UnknownOption: true}},
		{"rm", []string{"-r"}, false, DeleteExtent{Targets: []string{""}, Recursive: true}},
		{"rm", []string{"--", "-r", "tree"}, false, DeleteExtent{}},
		{"Remove-Item", []string{"-Recurse", "-Force", "-LiteralPath", "tree"}, true, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"ri", []string{"-r", "-Path", "tree"}, true, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"rm", []string{"-Recurse:$false", "tree"}, true, DeleteExtent{}},
		{"del", []string{"-Recurse:$true", "-ErrorAction", "Stop", "tree"}, true, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"", []string{"-rec", "tree"}, true, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"rd", []string{"/S", "/Q", "/F", "tree"}, true, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"rmdir", []string{"/s", "/unknown", "tree"}, true, DeleteExtent{Targets: []string{"tree"}, Recursive: true, UnknownOption: true}},
		{"erase", []string{"/s", "tree"}, true, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"cmd", nil, false, DeleteExtent{}},
		{"cmd", []string{"/k", "rd /s tree"}, false, DeleteExtent{}},
		{"cmd", []string{"/c"}, false, DeleteExtent{}},
		{"cmd", []string{"/c", ""}, false, DeleteExtent{}},
		{"cmd", []string{"/C", "rd /s /q tree"}, false, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"cmd", []string{"/c", "rd", "/s", "tree"}, false, DeleteExtent{Targets: []string{"tree"}, Recursive: true}},
		{"cmd", []string{"/c", "rd", "/s", "%TARGET%"}, false, DeleteExtent{Recursive: true, DynamicCommand: true}},
		{"xargs", []string{"-0", "rm", "-r"}, false, DeleteExtent{Targets: []string{""}, Recursive: true}},
		{"xargs", []string{"echo", "tree"}, false, DeleteExtent{}},
		{"find", nil, false, DeleteExtent{Targets: []string{"."}}},
		{"find", []string{"tree", "other", "-delete"}, false, DeleteExtent{Targets: []string{"tree", "other"}, Recursive: true}},
		{"find", []string{"-L", "-delete"}, false, DeleteExtent{Targets: []string{"."}, Recursive: true, UnknownOption: true}},
		{"find", []string{"tree", "-follow", "-delete"}, false, DeleteExtent{Targets: []string{"tree"}, Recursive: true, UnknownOption: true}},
		{"find", []string{"tree", "-exec"}, false, DeleteExtent{Targets: []string{"tree"}}},
		{"find", []string{"tree", "-exec", "echo", "{}", ";"}, false, DeleteExtent{Targets: []string{"tree"}}},
		{"find", []string{"tree", "-exec", "rm", "-r", "{}", "other", ";"}, false, DeleteExtent{Targets: []string{"tree", "other"}, Recursive: true}},
		{"find", []string{"tree", "-execdir", "rm", "-rZ", "{}", "+"}, false, DeleteExtent{Targets: []string{"tree", ""}, Recursive: true, UnknownOption: true}},
		{"sudo", []string{"--unknown", "rm", "-r", "tree"}, false, DeleteExtent{Targets: []string{""}, Recursive: true}},
		{"sudo", []string{"--unknown", "echo"}, false, DeleteExtent{}},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+joinDeleteArgs(tt.args), func(t *testing.T) {
			if got := AnalyzeRecursiveDelete(tt.name, tt.args, tt.ps); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDeleteWrappers(t *testing.T) {
	for _, wrapper := range []string{"sudo", "env", "command", "exec", "nohup", "builtin"} {
		t.Run(wrapper, func(t *testing.T) {
			args := []string{"rm", "-rf", "tree"}
			base, rest, known := UnwrapDeleteCommand(wrapper, args)
			if base != "rm" || !known || !reflect.DeepEqual(rest, args[1:]) {
				t.Fatalf("got %q, %v, %v", base, rest, known)
			}
			want := DeleteExtent{Targets: []string{"tree"}, Recursive: true}
			if got := AnalyzeRecursiveDelete(wrapper, args, false); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
	tests := []struct {
		name  string
		args  []string
		base  string
		rest  []string
		known bool
	}{
		{"sudo", nil, "", nil, true},
		{"sudo", []string{"-u"}, "", []string{"-u"}, false},
		{"env", []string{""}, "", []string{""}, false},
		{"sudo", []string{"--bad", "rm"}, "", []string{"--bad", "rm"}, false},
		{"command", []string{"-v", "rm"}, "", nil, true},
		{"command", []string{"-V", "rm"}, "", nil, true},
		{"command", []string{"-p", "--", "rm", "-r", "tree"}, "rm", []string{"-r", "tree"}, true},
		{"env", []string{"-i", "--ignore-environment", "A=value", "rm", "-r", "tree"}, "rm", []string{"-r", "tree"}, true},
		{"sudo", []string{"-E", "-n", "--non-interactive", "-u", "user", "-g", "group", "--user", "user", "--group", "group", "env", "A=value", "rm", "-r", "tree"}, "rm", []string{"-r", "tree"}, true},
		{"sudo", []string{"--"}, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+joinDeleteArgs(tt.args), func(t *testing.T) {
			base, rest, known := UnwrapDeleteCommand(tt.name, tt.args)
			if base != tt.base || known != tt.known || !reflect.DeepEqual(rest, tt.rest) {
				t.Fatalf("got %q, %#v, %v; want %q, %#v, %v", base, rest, known, tt.base, tt.rest, tt.known)
			}
		})
	}
}

func joinDeleteArgs(args []string) string {
	return strings.Join(args, " ")
}

func TestTimedRecursiveDeleteCalls(t *testing.T) {
	for _, source := range []string{"time rm -rf tree", "time -p sudo rm -R tree"} {
		t.Run(source, func(t *testing.T) {
			analysis, err := shellparse.AnalyzeDeleteCalls(source)
			if err != nil {
				t.Fatal(err)
			}
			if len(analysis.Calls) != 2 || !analysis.Calls[0].Unsafe || !analysis.Calls[1].Unsafe {
				t.Fatalf("got %#v, want unsafe timed call structure", analysis)
			}
			call := analysis.Calls[1]
			want := DeleteExtent{Targets: []string{"tree"}, Recursive: true}
			if got := AnalyzeRecursiveDelete(call.Name, call.Args, false); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}
