//go:build linux

package checkenv

import (
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"sync"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/safety/evidence"
)

type Invocation struct {
	mu          sync.Mutex
	spec        Spec
	identity    string
	command     string
	executionID string
	callID      string
	state       invocationState
	scope       *namespaceScope
	captured    *evidence.TestJSONObserver
}

type invocationState struct {
	directory      string
	runtimeRoot    string
	sourceRoot     string
	argv           []string
	input          evidence.CheckInputs
	runtimeDigest  string
	sourceDigest   string
	originalDigest string
	status         *os.File
	cmd            *exec.Cmd
	ctx            context.Context
	sourcePolicy   treePolicy
	launcher       fileutil.ContentVersion
	release        func()
	active         bool
	closed         bool
}

// New allocates no filesystem or process resources. Freezing happens only in
// the exact call's execution phase, after permissions and pre-tool hooks.
func New(spec Spec, command, executionID, callID string) *Invocation {
	i := &Invocation{spec: spec, command: command, executionID: executionID, callID: callID, identity: "venv_" + rand.Text(), scope: newNamespaceScope()}
	if spec.capture != nil {
		i.captured = evidence.NewTestJSONObserver(evidence.EvaluableCriterionNames(spec.capture.body))
	}
	return i
}

func (i *Invocation) Write(body []byte) (int, error) {
	if i.captured != nil {
		return i.captured.Write(body)
	}
	return len(body), nil
}

func (i *Invocation) CapturedOutcome() evidence.CriterionResult {
	if i.captured != nil {
		return i.captured.Outcome()
	}
	return evidence.CriterionUnavailable
}

func (i *Invocation) Inputs() []evidence.CheckInputs {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state.input.CommandDigest == "" {
		return nil
	}
	input := i.state.input
	if i.state.active && i.scope.stranded() {
		input.Gaps = []string{namespaceGap(i.scope.wait())}
	}
	input.Gaps = append([]string(nil), input.Gaps...)
	return []evidence.CheckInputs{input}
}

func (i *Invocation) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.state.closed = true
	if !i.state.active || i.scope.stranded() {
		i.remove()
	}
}

func (i *Invocation) remove() {
	i.scope.release()
	if i.state.status != nil {
		_ = i.state.status.Close()
		i.state.status = nil
	}
	if i.state.directory != "" {
		removeFrozenTree(i.state.directory)
		i.state.directory = ""
	}
	if i.state.release != nil {
		i.state.release()
		i.state.release = nil
	}
}
