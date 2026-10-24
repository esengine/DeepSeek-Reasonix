package checkenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

type containerEnvironment struct {
	mu                                 sync.Mutex
	spec                               Spec
	identity, command, execution, call string
	client                             *containerClient
	containerFiles
	policy                     treePolicy
	journal                    containerJournal
	release                    func()
	argv                       []string
	input                      evidence.CheckInputs
	cmd                        *exec.Cmd
	ctx                        context.Context
	controlRead, controlWrite  *os.File
	done                       chan struct{}
	cliWaited                  chan struct{}
	waitOnce                   sync.Once
	scopeErr                   error
	exitCode                   int
	started, cancelled, closed bool
	captured                   *evidence.TestJSONObserver
}

type containerFiles struct {
	directory, source, sourcePath, sourceDigest, originalDigest string
}

func newContainerEnvironment(spec Spec, command, executionID, callID string) *containerEnvironment {
	spec.Client.Environment = append([]string(nil), spec.Client.Environment...)
	i := &containerEnvironment{spec: spec, command: command, execution: executionID, call: callID,
		identity: "check-" + strings.ToLower(rand.Text()), done: make(chan struct{}), cliWaited: make(chan struct{}), exitCode: -1}
	if spec.capture != nil {
		i.captured = evidence.NewTestJSONObserver(evidence.EvaluableCriterionNames(spec.capture.body))
	}
	return i
}

func containerImageID(uri string) (string, error) {
	id, ok := strings.CutPrefix(uri, "docker-image://sha256:")
	decoded, err := hex.DecodeString(id)
	if !ok || err != nil || len(decoded) != 32 || id != strings.ToLower(id) {
		return "", errors.Join(ErrUnsupported, errors.New("verification requires a local immutable sha256 image ID"))
	}
	return "sha256:" + id, nil
}

func (i *containerEnvironment) Write(body []byte) (int, error) {
	if i.captured != nil {
		return i.captured.Write(body)
	}
	return len(body), nil
}

func (i *containerEnvironment) CapturedOutcome() evidence.CriterionResult {
	if i.captured != nil {
		return i.captured.Outcome()
	}
	return evidence.CriterionUnavailable
}

func (i *containerEnvironment) Inputs() []evidence.CheckInputs {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.input.CommandDigest == "" {
		return nil
	}
	input := i.input
	input.Gaps = append([]string(nil), input.Gaps...)
	return []evidence.CheckInputs{input}
}

func (i *containerEnvironment) Command(ctx context.Context, launch tool.ShellLaunch) (*exec.Cmd, func(), error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	directory, err := filepath.EvalSymlinks(launch.Directory)
	if err != nil || i.closed || i.cmd != nil || len(i.argv) == 0 || launch.Command != i.command || directory != i.spec.Directory ||
		!reflect.DeepEqual(launch.Arguments, i.argv) {
		return nil, nil, errors.New("verification launch identity changed")
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmd := i.client.command(ctx, "start", "--attach", "--interactive", i.journal.RunID)
	cmd.Stdin, cmd.Cancel = read, i.Cancel
	i.controlRead, i.controlWrite, i.cmd, i.ctx = read, write, cmd, ctx
	i.input.ArgumentsDigest = digest(cmd.Args)
	i.input.EnvironmentHash = digest(struct {
		Environment                  []string
		Engine                       containerEngine
		Image, Source, Configuration string
	}{
		cmd.Environ(), i.client.engine, i.journal.Image, i.sourceDigest, i.journal.Configuration})
	var once sync.Once
	return cmd, func() { once.Do(i.finish) }, nil
}

func (i *containerEnvironment) Close() {
	i.mu.Lock()
	i.closed = true
	active := i.cmd != nil && i.started
	i.mu.Unlock()
	if active {
		_ = i.Cancel()
		_ = i.WaitScope()
	}
	i.cleanup()
}
