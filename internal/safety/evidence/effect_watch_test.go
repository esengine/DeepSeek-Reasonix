package evidence

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func existsIn(files ...string) Exists {
	set := map[string]bool{}
	for _, f := range files {
		set[filepath.Clean(filepath.Join("/ws", f))] = true
	}
	return ExistsUnder("/ws", func(p string) bool { return set[filepath.Clean(p)] })
}

func shellReceipt(command string, output int) Receipt {
	return Receipt{ToolName: "bash", Command: command, Success: true, OutputBytes: output}
}

func TestEffectWatchVaryingEchoIsNoEffect(t *testing.T) {
	w := NewEffectWatch()
	exists := existsIn("main.go")
	for _, cmd := range []string{"echo step 1", "echo step 2", `echo "doing it"`, "printf 'x %s' 3"} {
		if w.RoundHadEffect([]Receipt{shellReceipt(cmd, 12)}, exists) {
			t.Fatalf("%q counted as an effect", cmd)
		}
	}
}

func TestEffectWatchShellReadOfNewPathCountsOnce(t *testing.T) {
	w := NewEffectWatch()
	exists := existsIn("a.go", "b.go")
	if !w.RoundHadEffect([]Receipt{shellReceipt("cat a.go | head -n 20", 40)}, exists) {
		t.Fatal("first read of a.go through a pipeline was not an effect")
	}
	if w.RoundHadEffect([]Receipt{shellReceipt("sed -n 1,5p a.go", 40)}, exists) {
		t.Fatal("reading a.go again counted as new")
	}
	if !w.RoundHadEffect([]Receipt{shellReceipt("grep -n foo b.go", 40)}, exists) {
		t.Fatal("first read of b.go was not an effect")
	}
	if w.RoundHadEffect([]Receipt{shellReceipt("cat missing.go", 0)}, exists) {
		t.Fatal("a command with no output counted as a read")
	}
}

func TestEffectWatchReadToolRequestsAreIdentities(t *testing.T) {
	w := NewEffectWatch()
	read := func(args string) Receipt {
		return Receipt{ToolName: "web_fetch", Args: json.RawMessage(args), Success: true, Read: true, OutputBytes: 100}
	}
	if !w.RoundHadEffect([]Receipt{read(`{"url":"https://a.example"}`)}, nil) {
		t.Fatal("a new URL was not an effect")
	}
	if w.RoundHadEffect([]Receipt{read(`{"url":"https://a.example"}`)}, nil) {
		t.Fatal("the same URL again was an effect")
	}
	if !w.RoundHadEffect([]Receipt{read(`{"url":"https://b.example"}`)}, nil) {
		t.Fatal("a second new URL was not an effect")
	}
}

func TestEffectWatchStateAndDelegation(t *testing.T) {
	cases := []Receipt{
		{ToolName: "write_file", Success: true, Write: true},
		{ToolName: "bash", Command: "make gen", Success: true, Mutation: true},
		{ToolName: "complete_step", Success: true, StepProof: true},
		{ToolName: "task", Success: true, OutputBytes: 10},
	}
	for _, r := range cases {
		if !NewEffectWatch().RoundHadEffect([]Receipt{r}, nil) {
			t.Fatalf("%+v was not an effect", r)
		}
	}
	if NewEffectWatch().RoundHadEffect([]Receipt{{ToolName: "write_file", Write: true}}, nil) {
		t.Fatal("a failed write counted as an effect")
	}
}

func TestEffectWatchCheckMovingIsAnEffectRepeatingIsNot(t *testing.T) {
	code := func(n int) *int { return &n }
	run := func(exit int) Receipt {
		return Receipt{ToolName: "bash", Command: "go test ./...", Success: exit == 0, ExitCode: code(exit), OutputBytes: 30}
	}
	w := NewEffectWatch()
	if w.RoundHadEffect([]Receipt{run(1)}, nil) {
		t.Fatal("the first run of a check is a baseline, not a change")
	}
	if w.RoundHadEffect([]Receipt{run(1)}, nil) {
		t.Fatal("a check failing the same way again counted as an effect")
	}
	if !w.RoundHadEffect([]Receipt{run(0)}, nil) {
		t.Fatal("a check turning green was not an effect")
	}
}
