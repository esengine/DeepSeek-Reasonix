package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/safety/sandbox"
)

func TestMemoryBenchRequiresReadSandbox(t *testing.T) {
	tasks, err := loadTasks("../../benchmarks/memorybench")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 {
		t.Fatal("MemoryBench corpus is empty")
	}
	for _, task := range tasks {
		if task.answerRoot == "" {
			t.Fatalf("%s can run without the answer boundary", task.ID)
		}
	}
	err = validateAnswerIsolation(tasks)
	if sandbox.Available() && err != nil {
		t.Fatal(err)
	}
	if !sandbox.Available() && err == nil {
		t.Fatal("MemoryBench accepted a host with no OS read sandbox")
	}
	if sandbox.Available() {
		previous := benchStateBase
		benchStateBase = func() (string, error) { return filepath.Join(tasks[0].answerRoot, "cache"), nil }
		t.Cleanup(func() { benchStateBase = previous })
		if err := validateAnswerIsolation(tasks); err == nil {
			t.Fatal("MemoryBench accepted a state root inside the forbidden checkout")
		}
	}
}

func TestMemoryBenchAnswerIsolationAtSandboxBoundary(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("this host has no OS read sandbox")
	}
	const suite = "../../benchmarks/memorybench"
	tasks, err := loadTasks(suite)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 || tasks[0].answerRoot == "" {
		t.Fatal("MemoryBench answer material was not identified")
	}
	if err := validateAnswerIsolation(tasks); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := stageAnswerIsolation(tasks[0].answerRoot, work); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[sandbox]\nbash = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_HOME", home)
	cfg, err := config.LoadForRoot(work)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BashMode() != "enforce" {
		t.Fatalf("sandbox mode = %q, want enforce even when the user config disables it", cfg.BashMode())
	}
	visible := filepath.Join(work, "visible.txt")
	if err := os.WriteFile(visible, []byte("visible-control"), 0o600); err != nil {
		t.Fatal(err)
	}
	answer := filepath.Join(suite, "tasks", "mb-contradiction", "memory", "project", "package-manager.md")
	answer, err = filepath.Abs(answer)
	if err != nil {
		t.Fatal(err)
	}
	spec := sandbox.Spec{Mode: cfg.BashMode(), WriteRoots: []string{work}, ForbidReadRoots: cfg.ForbidReadRootsForRoot(work)}
	read := func(path string) (string, error) {
		t.Helper()
		argv, wrapped := sandbox.CommandArgs(spec, []string{"cat", path})
		if !wrapped {
			t.Fatal("OS sandbox did not wrap the command")
		}
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		return string(out), err
	}
	if got, err := read(visible); err != nil || !strings.Contains(got, "visible-control") {
		t.Fatalf("control file read = %q, %v", got, err)
	}
	if got, err := read(answer); err == nil || strings.Contains(got, "npm install") {
		t.Fatalf("answer file remained readable through the sandbox: %q, %v", got, err)
	}
}

func TestAnswerIsolationRefusesUnsafeWorkdirAndExistingConfig(t *testing.T) {
	const answerRoot = "../../benchmarks/memorybench/tasks"
	if err := stageAnswerIsolation(answerRoot, filepath.Dir(answerRoot)); err == nil {
		t.Fatal("workdir inside the checkout was accepted")
	}
	work := t.TempDir()
	path := filepath.Join(work, "reasonix.toml")
	if err := os.WriteFile(path, []byte("# task fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stageAnswerIsolation(answerRoot, work); err == nil {
		t.Fatal("existing task config was overwritten")
	}
	if data, _ := os.ReadFile(path); string(data) != "# task fixture\n" {
		t.Fatalf("task config changed: %q", data)
	}
}

func TestRunTaskDropsIsolationConfigBeforeGrading(t *testing.T) {
	requireShellStub(t)
	suite := t.TempDir()
	taskDir := filepath.Join(suite, "tasks", "demo")
	if err := os.MkdirAll(filepath.Join(taskDir, "memory", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	verify := "#!/usr/bin/env bash\nset -e\ntest ! -e reasonix.toml\ngrep -q '^ok$' answer.txt\n"
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte(verify), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "fake-agent")
	script := `#!/usr/bin/env bash
set -e
test -f reasonix.toml
printf 'ok\n' > answer.txt
while [ "$#" -gt 0 ]; do
  case "$1" in
    --metrics) printf '{"complete":true}\n' > "$2"; shift 2 ;;
    --trajectory) printf '{"event":{"kind":"text","text":"done"}}\n' > "$2"; shift 2 ;;
    *) shift ;;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	tk := task{ID: "demo", Prompt: "write the answer", TimeoutSec: 30, dir: taskDir, answerRoot: filepath.Join(suite, "tasks")}
	r := runTask(suiteConfig{bin: bin}, tk)
	if !r.Passed {
		t.Fatalf("isolated task failed grading: %+v", r)
	}
}

func TestMemoryBenchRejectsAnswerReadInTrajectory(t *testing.T) {
	requireShellStub(t)
	suite := t.TempDir()
	taskDir := filepath.Join(suite, "tasks", "demo")
	answer := filepath.Join(taskDir, "memory", "project", "fact.md")
	if err := os.MkdirAll(filepath.Dir(answer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(answer, []byte("---\nname: fact\n---\nA unique answer that the graded child must not read.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte("#!/bin/sh\ntest -f answer.txt\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "fake-agent")
	script := `#!/usr/bin/env bash
set -e
printf 'ok\n' > answer.txt
while [ "$#" -gt 0 ]; do
  case "$1" in
    --metrics) printf '{"complete":true}\n' > "$2"; shift 2 ;;
    --trajectory)
      printf '{"event":{"kind":"tool_dispatch","tool":{"id":"read-1","name":"read_file","args":"{\\"path\\":\\"%s\\"}"}}}\n' "$ANSWER_FILE" > "$2"
      printf '{"event":{"kind":"tool_result","tool":{"id":"read-1","name":"read_file","output":"A unique answer that the graded child must not read."}}}\n' >> "$2"
      shift 2 ;;
    *) shift ;;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANSWER_FILE", answer)
	tk := task{ID: "demo", Prompt: "write the answer", TimeoutSec: 30, dir: taskDir, answerRoot: filepath.Join(suite, "tasks")}
	r := runTask(suiteConfig{bin: bin}, tk)
	if r.Passed || !errors.Is(r.answerAuditErr, errAnswerRead) {
		t.Fatalf("answer-read trajectory was accepted: %+v", r)
	}
}

func TestMissingAnswerTrajectoryKeepsOtherTasksRunning(t *testing.T) {
	requireShellStub(t)
	root := filepath.Join(t.TempDir(), "tasks")
	var tasks []task
	for _, id := range []string{"missing", "complete"} {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "verify.sh"), []byte("#!/bin/sh\ntest -f answer.txt\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task{ID: id, Prompt: id, TimeoutSec: 30, dir: dir, answerRoot: root})
	}
	bin := filepath.Join(t.TempDir(), "fake-agent")
	script := `#!/usr/bin/env bash
set -e
printf 'ok\n' > answer.txt
traj=''
prompt=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --metrics) printf '{"complete":true}\n' > "$2"; shift 2 ;;
    --trajectory) traj="$2"; shift 2 ;;
    *) prompt="$1"; shift ;;
  esac
done
if [ "$prompt" = complete ]; then
  printf '{"event":{"kind":"text","text":"done"}}\n' > "$traj"
fi
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	results := runSuite(suiteConfig{bin: bin}, tasks)
	if len(results) != 2 || !errors.Is(results[0].answerAuditErr, errAnswerAuditIncomplete) || !results[1].Passed {
		t.Fatalf("missing trajectory stopped the suite: %+v", results)
	}
	results[0].Outcome = "renamed display status"
	if !answerAuditFailed(results[0]) || answerLeakDetected(results[0]) {
		t.Fatal("audit control flow depended on the display outcome")
	}
}

func TestAnswerTrajectoryAuditDistinguishesReadsFromListingsAndDenials(t *testing.T) {
	const fact = "A unique answer that the graded child must not read."
	const dispatch = `{"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash","args":"{\"command\":\"cat /repo/tasks/demo/memory/project/fact.md\"}"}}}`
	for _, tc := range []struct {
		name, data string
		want       bool
	}{
		{"successful shell read", dispatch + "\n" + `{"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","output":"` + fact + `","execution":{"exitCode":0}}}}`, true},
		{"sandbox denial", dispatch + "\n" + `{"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","output":"` + fact + `","execution":{"exitCode":1}}}}`, false},
		{"directory listing", `{"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash","args":"{\"command\":\"ls /repo/tasks/demo/memory\"}"}}}` + "\n" + `{"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","output":"fact.md"}}}`, false},
		{"native read denial", `{"event":{"kind":"tool_result","tool":{"name":"read_file","args":"{\"path\":\"/repo/tasks/demo/memory/project/fact.md\"}","err":"sandbox denied"}}}`, false},
		{"grep reads fact", `{"event":{"kind":"tool_result","tool":{"name":"grep","args":"{\"path\":\"/repo/tasks/demo/memory/project/fact.md\",\"pattern\":\"unique\"}","output":"` + fact + `"}}}`, true},
		{"glob lists name only", `{"event":{"kind":"tool_result","tool":{"name":"glob","args":"{\"pattern\":\"/repo/tasks/demo/memory/project/*.md\"}","output":"/repo/tasks/demo/memory/project/fact.md"}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run.trajectory.jsonl")
			if err := os.WriteFile(path, []byte(tc.data+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := scanAnswerRead(path, []string{fact})
			if err != nil || got != tc.want {
				t.Fatalf("scanAnswerRead = %t, %v; want %t", got, err, tc.want)
			}
		})
	}
}

func TestAnswerTrajectoryAuditScansEarlierSegments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	answer := filepath.Join(root, "demo", "memory", "project", "fact.md")
	if err := os.MkdirAll(filepath.Dir(answer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(answer, []byte("---\nname: fact\n---\nA fact body in the first trajectory leg.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for index, data := range []string{
		`{"event":{"kind":"tool_result","tool":{"name":"bash","args":"{\"command\":\"cat /repo/tasks/demo/memory/project/fact.md\"}","output":"A fact body in the first trajectory leg."}}}`,
		`{"event":{"kind":"text","text":"done"}}`,
	} {
		path := segmentTrajectoryPath(dir, "demo", segment{index: index + 1}, 2)
		if err := os.WriteFile(path, []byte(data+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read, err := auditAnswerTrajectories(dir, task{ID: "demo", answerRoot: root}, 2)
	if err != nil || !read {
		t.Fatalf("earlier answer read was missed: read=%t err=%v", read, err)
	}
}
