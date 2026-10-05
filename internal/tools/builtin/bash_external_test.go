package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// A dropped external folder is reachable by the read tools and by nothing else.
// The shell's own answer for its token is "No such file or directory", which a
// model reads as proof the file does not exist — it then tells the user the
// reference is fake and stops, with the file sitting there the whole time.
func TestBashRefusesATokenItCannotSeeAndNamesWhatCan(t *testing.T) {
	root := testenv.TempDir(t)
	paths := NewPathResolver()
	paths.RegisterReadRoot("__reasonix_external_folder/6812c34ccbd3/Desktop", root)
	b := bash{paths: paths}

	cmd := `ls -la "__reasonix_external_folder/6812c34ccbd3/Desktop/notes.md" 2>&1; echo "---"`
	res, err := b.ExecuteDetailed(context.Background(), json.RawMessage(`{"command":`+quote(cmd)+`}`))
	if err == nil {
		t.Fatal("bash ran a command naming an external token; the shell cannot see it and answers as if the file were gone")
	}
	if res.Execution == nil || res.Execution.State != "not_run" {
		t.Fatalf("execution state = %+v, want the call recorded as never started", res.Execution)
	}
	for _, want := range []string{"__reasonix_external_folder/6812c34ccbd3/Desktop", "read_file"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q does not name %q: a refusal that does not point somewhere is the dead end again", err, want)
		}
	}
}

// The registry is what makes this recognition rather than a guess: a command is
// refused because it names a token this session actually minted, not because it
// looks like one.
func TestBashLetsThroughCommandsThatNameNoRegisteredToken(t *testing.T) {
	paths := NewPathResolver()
	paths.RegisterReadRoot("__reasonix_external_folder/6812c34ccbd3/Desktop", testenv.TempDir(t))
	b := bash{paths: paths}

	for _, cmd := range []string{
		"ls -la /Users/yhh/Desktop",
		"echo __reasonix_external_folder/0000000000/Desktop",
		"git status",
	} {
		if err := b.refuseExternalRef(cmd); err != nil {
			t.Fatalf("refused %q: %v", cmd, err)
		}
	}
}

// Most sessions never mount one, and a resolver that was never handed a root
// must not start refusing shell commands.
func TestBashWithoutAnyExternalRootRefusesNothing(t *testing.T) {
	for _, b := range []bash{{paths: NewPathResolver()}, {}} {
		if err := b.refuseExternalRef("ls __reasonix_external_folder/x/y"); err != nil {
			t.Fatalf("refused with no root registered: %v", err)
		}
	}
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
