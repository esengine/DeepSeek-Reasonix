package checkenv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/tools/shellrun"
)

func TestContainerHostDeathStopsScopeAndRecoversOnlyOwnedObjects(t *testing.T) {
	spec := containerFixture(t)
	witness := filepath.Join(t.TempDir(), "admitted.json")
	body, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(t.Context(), executable, "-test.run=^TestContainerHostDeathChild$")
	child.Env = append(os.Environ(), "REASONIX_CHECK_CRASH_SPEC="+file, "REASONIX_CHECK_CRASH_WITNESS="+witness)
	output, err := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("host did not die at admitted payload: %v %s", err, output)
	}
	raw, err := os.ReadFile(witness)
	if err != nil {
		t.Fatal(err)
	}
	var journal containerJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	client, err := newContainerClient(t.Context(), t.TempDir(), spec.Client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		object, err := client.inspect(ctx, journal.Run)
		if err != nil {
			t.Fatal(err)
		}
		if !object.State.Running && object.State.Pid == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("host death left a running container scope")
		case <-ticker.C:
		}
	}
	directory := filepath.Join(spec.StorageRoot, journal.Identity)
	if _, err := os.Stat(filepath.Join(directory, "container.json")); err != nil {
		t.Fatal("crash discarded recovery record", err)
	}
	if err := recoverContainerJournal(t.Context(), directory, client); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("owned journal not recovered: %v", err)
	}
	body, err = client.run(t.Context(), nil, "container", "ls", "--all", "--quiet", "--filter", "label="+containerOwnerLabel+"="+journal.Identity)
	if err != nil || strings.TrimSpace(string(body)) != "" {
		t.Fatalf("owned container survived recovery: %s %v", body, err)
	}
}

func TestContainerHostDeathChild(t *testing.T) {
	file := os.Getenv("REASONIX_CHECK_CRASH_SPEC")
	if file == "" {
		return
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var spec Spec
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}
	command := `printf 'admitted\n'; while :; do :; done`
	i := newContainerEnvironment(spec, command, "crash-execution", "crash-call")
	argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: command, Directory: spec.Directory})
	if err != nil {
		t.Fatal(err)
	}
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: argv, Dir: spec.Directory, Command: command, Track: true, Progress: func(output string) {
		if strings.Contains(output, "admitted") {
			body, err := json.Marshal(i.journal)
			if err != nil {
				panic(err)
			}
			if err := os.WriteFile(os.Getenv("REASONIX_CHECK_CRASH_WITNESS"), body, 0600); err != nil {
				panic(err)
			}
			os.Exit(77)
		}
	}})
	t.Fatalf("crash witness not reached: %v %s", result.Err, result.Combined)
}

func TestContainerRecoveryPreservesUnknownVersionAndActiveOwner(t *testing.T) {
	for _, body := range []string{`{"version":999,"future_state":{"retain":true}}`, `{"version":1,"future_state":{"retain":true}}`, `{"version":1} {"future_state":true}`} {
		t.Run(body, func(t *testing.T) { containerRecoveryPreservesUnsupported(t, []byte(body)) })
	}
}

func containerRecoveryPreservesUnsupported(t *testing.T, body []byte) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "check-fixture")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "container.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	release, err := filelock.TryAcquire(filepath.Join(directory, "owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := recoverContainerJournal(t.Context(), directory, nil); err != nil {
		t.Fatal(err)
	}
	release()
	if err := recoverContainerJournal(t.Context(), directory, nil); err == nil {
		t.Fatal("unknown recovery version was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(body) {
		t.Fatal("unknown journal changed", err)
	}
}

func TestContainerInputChangeInvalidatesSuccessfulCheck(t *testing.T) {
	spec := containerFixture(t)
	command := `[[ $(< input) == frozen ]] || exit 11; printf 'observed\n'`
	i := NewEnvironment(spec, command, "changed-execution", "changed-call")
	t.Cleanup(i.Close)
	argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: command, Directory: spec.Directory})
	if err != nil {
		t.Fatal(err)
	}
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: argv, Dir: spec.Directory, Command: command, Track: true, Progress: func(body string) {
		if strings.Contains(body, "observed") {
			if err := os.WriteFile(filepath.Join(spec.SourceRoot, "input"), []byte("external change"), 0600); err != nil {
				panic(err)
			}
		}
	}})
	if result.Err != nil {
		t.Fatal(result.Err, result.Combined)
	}
	inputs := i.Inputs()
	if len(inputs) != 1 || inputs[0].Stable || Verify(inputs[0], "changed-execution") {
		t.Fatalf("external change reused receipt: %+v", inputs)
	}
}

func TestContainerCapturedGoCheckUsesHostCriterion(t *testing.T) {
	spec := containerFixture(t)
	criterion := filepath.Join(spec.SourceRoot, "criterion_test.go")
	for path, body := range map[string]string{"go.mod": "module fixture\n\ngo 1.26.0\n", "criterion_test.go": "package fixture\nimport \"testing\"\nfunc TestCaptured(t *testing.T){t.Fatal(\"candidate changed criterion\")}\n"} {
		if err := os.WriteFile(filepath.Join(spec.SourceRoot, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	body := []byte("package fixture\nimport \"testing\"\nfunc TestCaptured(t *testing.T){}\n")
	command := `GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOCACHE=/tmp/go-cache go test -json -count=1 ./...`
	i := NewEnvironment(spec.WithCaptured(criterion, body), command, "captured-execution", "captured-call")
	t.Cleanup(i.Close)
	argv, err := i.Prepare(t.Context(), tool.ControlledShellRequest{Command: command, Directory: spec.Directory})
	if err != nil {
		t.Fatal(err)
	}
	result := shellrun.RunForeground(tool.WithControlledShell(t.Context(), i), shellrun.Request{Argv: argv, Dir: spec.Directory, Command: command, Track: true})
	inputs := i.Inputs()
	if result.Err != nil || len(inputs) != 1 || i.CapturedOutcome() != evidence.CriterionPass || !VerifyCaptured(inputs[0], "captured-execution", criterion, body) {
		t.Fatalf("captured criterion not proved: %v %s %+v", result.Err, result.Combined, inputs)
	}
	if Verify(inputs[0], "captured-execution") {
		t.Fatal("captured check authorized ordinary integration")
	}
	current, err := os.ReadFile(criterion)
	if err != nil || !strings.Contains(string(current), "candidate changed criterion") {
		t.Fatal("criterion rewrote host source", err)
	}
}

func TestOneUnrecoverableJournalDoesNotBlockLaterChecks(t *testing.T) {
	storage := t.TempDir()
	stale := filepath.Join(storage, "check-newer")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"version":999}`)
	if err := os.WriteFile(filepath.Join(stale, "container.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverContainerJournals(t.Context(), storage, filepath.Join(storage, "check-current"), nil); err != nil {
		t.Fatalf("a retained journal blocked every later check: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(stale, "container.json")); err != nil || string(after) != string(body) {
		t.Fatal("unrecoverable journal was not retained", err)
	}
}
