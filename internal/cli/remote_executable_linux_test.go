//go:build linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
)

const remoteExecutableVersion = "v0.0.0-remote-executable-test"

func TestReasonixExecutableRemoteServeAttachStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the production reasonix executable")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	testRoot := t.TempDir()
	binary := filepath.Join(testRoot, "reasonix")
	revision := strings.Repeat("7", 40)
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	build := exec.Command(goTool, "build", "-trimpath",
		"-ldflags=-X main.version="+remoteExecutableVersion+" -X reasonix/internal/buildinfo.SourceRevision="+revision,
		"-o", binary, "./cmd/reasonix")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build reasonix executable: %v\n%s", err, output)
	}

	home := filepath.Join(testRoot, "home")
	reasonixHome := filepath.Join(testRoot, "reasonix-home")
	configHome := filepath.Join(testRoot, "config")
	runtimeDir := filepath.Join(testRoot, "runtime")
	unitPath := filepath.Join(configHome, "systemd", "user", service.UnitName)
	for _, directory := range []string{home, reasonixHome, runtimeDir, filepath.Dir(unitPath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// attach intentionally refuses to connect unless the user explicitly
	// installed the service. The executable smoke owns only this isolated marker;
	// it does not invoke or emulate systemctl.
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart="+binary+" remote serve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	processEnvironment := append(os.Environ(),
		"HOME="+home,
		"REASONIX_HOME="+reasonixHome,
		"XDG_CONFIG_HOME="+configHome,
		"XDG_RUNTIME_DIR="+runtimeDir,
	)

	serveStdout, err := os.CreateTemp(testRoot, "serve-stdout-")
	if err != nil {
		t.Fatal(err)
	}
	defer serveStdout.Close()
	serveStderr, err := os.CreateTemp(testRoot, "serve-stderr-")
	if err != nil {
		t.Fatal(err)
	}
	defer serveStderr.Close()
	serve := exec.Command(binary, "remote", "serve")
	serve.Env = processEnvironment
	serve.Stdout = serveStdout
	serve.Stderr = serveStderr
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	serveWaited := false
	t.Cleanup(func() {
		if !serveWaited {
			_ = serve.Process.Kill()
			_ = serve.Wait()
		}
	})

	endpoint := service.NewEndpoint(service.Paths{RuntimeDir: runtimeDir, UnitPath: unitPath})
	deadline := time.Now().Add(10 * time.Second)
	for {
		probeContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		connection, probeErr := endpoint.Dial(probeContext)
		cancel()
		if probeErr == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			_ = serve.Process.Kill()
			waitErr := serve.Wait()
			serveWaited = true
			t.Fatalf("reasonix remote serve did not bind: %v (wait=%v, stderr=%s)", probeErr, waitErr, readProcessOutput(t, serveStderr))
		}
		time.Sleep(10 * time.Millisecond)
	}

	buildID, err := protocol.NewBuildID(remoteExecutableVersion, revision)
	if err != nil {
		t.Fatal(err)
	}
	firstEpoch := runExecutableAttachExchange(t, binary, processEnvironment, buildID)
	secondEpoch := runExecutableAttachExchange(t, binary, processEnvironment, buildID)
	if firstEpoch == "" || secondEpoch != firstEpoch {
		t.Fatalf("daemon epoch changed across attach process reconnect: first=%q second=%q", firstEpoch, secondEpoch)
	}

	if err := serve.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := serve.Wait(); err != nil {
		serveWaited = true
		t.Fatalf("reasonix remote serve exit: %v (stderr=%s)", err, readProcessOutput(t, serveStderr))
	}
	serveWaited = true
	if output := readProcessOutput(t, serveStdout); output != "" {
		t.Fatalf("reasonix remote serve polluted stdout: %q", output)
	}
}

func TestReasonixExecutableRemoteServeSIGKILLColdRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and crash-restarts the production reasonix executable")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	testRoot := t.TempDir()
	binary := filepath.Join(testRoot, "reasonix")
	revision := strings.Repeat("8", 40)
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	build := exec.Command(goTool, "build", "-trimpath",
		"-ldflags=-X main.version="+remoteExecutableVersion+" -X reasonix/internal/buildinfo.SourceRevision="+revision,
		"-o", binary, "./cmd/reasonix")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build reasonix executable: %v\n%s", err, output)
	}

	askArguments := `{"questions":[{"header":"Restart","question":"Choose after the daemon stays alive.","options":[{"label":"A"},{"label":"B"}]}]}`
	encodedAskArguments, err := json.Marshal(askArguments)
	if err != nil {
		t.Fatal(err)
	}
	toolFrame := fmt.Sprintf(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_before_crash","type":"function","function":{"name":"ask","arguments":%s}}]}}]}`+"\n\n", encodedAskArguments)
	providerRequests := make(chan []byte, 8)
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			body = []byte("read request: " + readErr.Error())
		}
		select {
		case providerRequests <- body:
		default:
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, toolFrame)
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	defer providerServer.Close()

	home := filepath.Join(testRoot, "home")
	reasonixHome := filepath.Join(testRoot, "reasonix-home")
	configHome := filepath.Join(testRoot, "config")
	runtimeDir := filepath.Join(testRoot, "runtime")
	workspace := filepath.Join(home, "workspace")
	unitPath := filepath.Join(configHome, "systemd", "user", service.UnitName)
	for _, directory := range []string{home, reasonixHome, runtimeDir, workspace, filepath.Dir(unitPath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart="+binary+" remote serve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectConfig := fmt.Sprintf(`default_model = "local/remote-crash-model"

[[providers]]
name = "local"
kind = "openai"
base_url = %q
model = "remote-crash-model"
api_key_env = "REASONIX_REMOTE_EXECUTABLE_TEST_KEY"
reasoning_protocol = "none"

[environment]
enabled = false
`, providerServer.URL)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(projectConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	processEnvironment := append(os.Environ(),
		"HOME="+home,
		"REASONIX_HOME="+reasonixHome,
		"XDG_CONFIG_HOME="+configHome,
		"XDG_RUNTIME_DIR="+runtimeDir,
		"REASONIX_REMOTE_EXECUTABLE_TEST_KEY=test-key",
		"REASONIX_SAFE_MODE=0",
	)
	endpoint := service.NewEndpoint(service.Paths{RuntimeDir: runtimeDir, UnitPath: unitPath})
	buildID, err := protocol.NewBuildID(remoteExecutableVersion, revision)
	if err != nil {
		t.Fatal(err)
	}

	firstServe := startExecutableRemoteServe(t, binary, processEnvironment, testRoot, "before-crash")
	waitExecutableRemoteEndpoint(t, endpoint, firstServe)
	firstAttach := startExecutableRemoteAttach(t, binary, processEnvironment, testRoot, "before-crash")
	firstInitialized := firstAttach.initialize(t, buildID, "desktop-crash-recovery-before")
	oldHostEpoch := firstInitialized.HostEpoch

	var browse protocol.WorkspaceBrowseResult
	firstAttach.requestOK(t, protocol.MethodWorkspaceBrowse, protocol.WorkspaceBrowseParams{
		ExpectedHostEpoch: oldHostEpoch, TypedPath: workspace,
	}, &browse)
	var opened protocol.WorkspaceOpenResult
	firstAttach.requestOK(t, protocol.MethodWorkspaceOpen, protocol.WorkspaceOpenParams{
		HostMutation:        protocol.HostMutation{RequestID: "crash-open", ExpectedHostEpoch: oldHostEpoch},
		PrimaryDirectoryRef: browse.Directory.DirectoryRef,
	}, &opened)
	model := "local/remote-crash-model"
	collaboration := protocol.CollaborationNormal
	tokenMode := protocol.TokenFull
	approvalMode := protocol.ToolApprovalAsk
	var created protocol.SessionCreateResult
	firstAttach.requestOK(t, protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "crash-create", ExpectedHostEpoch: oldHostEpoch},
		WorkspaceID:  opened.Workspace.WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew},
		Profile: protocol.ProfileSelection{
			Model: &model, CollaborationMode: &collaboration, TokenMode: &tokenMode, ToolApprovalMode: &approvalMode,
		},
	}, &created)
	var subscribed protocol.SessionSubscribeResult
	firstAttach.requestOK(t, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: oldHostEpoch, Target: created.Target, PageTurns: 60,
	}, &subscribed)
	var submitted protocol.SessionSubmitResult
	firstAttach.requestOK(t, protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "crash-submit", ExpectedHostEpoch: oldHostEpoch,
			Target: created.Target, ExpectedRuntimeEpoch: subscribed.Snapshot.RuntimeEpoch,
		},
		Input: "accepted before a real daemon SIGKILL", DisplayText: "accepted before a real daemon SIGKILL",
	}, &submitted)
	if submitted.Kind != protocol.SubmitTurn || submitted.TurnID == "" {
		t.Fatalf("session/submit = %+v, want accepted Turn", submitted)
	}
	select {
	case body := <-providerRequests:
		if !bytes.Contains(body, []byte(`"name":"ask"`)) {
			t.Fatalf("production provider request omitted ask tool: %s", body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("production runtime did not reach the local OpenAI-compatible provider")
	}

	pendingDeadline := time.Now().Add(10 * time.Second)
	for subscribed.Snapshot.PendingPrompt == nil && time.Now().Before(pendingDeadline) {
		var replacement protocol.SessionSubscribeResult
		firstAttach.requestOK(t, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
			ExpectedHostEpoch: oldHostEpoch, Target: created.Target, PageTurns: 60,
			ReplaceSubscriptionID: subscribed.SubscriptionID,
		}, &replacement)
		subscribed = replacement
		if subscribed.Snapshot.PendingPrompt == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if subscribed.Snapshot.PendingPrompt == nil || subscribed.Snapshot.PendingPrompt.Kind != protocol.PromptAsk ||
		subscribed.Snapshot.PendingPrompt.Ask == nil || subscribed.Snapshot.PendingPrompt.Ask.PromptID == "" {
		t.Fatalf("pre-crash snapshot has no pending Ask: %+v", subscribed.Snapshot)
	}
	if !subscribed.Snapshot.Runtime.Running || subscribed.Snapshot.Runtime.CurrentTurn == nil ||
		subscribed.Snapshot.Runtime.CurrentTurn.TurnID != submitted.TurnID {
		t.Fatalf("pre-crash Turn was not active: runtime=%+v submit=%+v", subscribed.Snapshot.Runtime, submitted)
	}
	oldRuntimeEpoch := subscribed.Snapshot.RuntimeEpoch
	oldPromptID := subscribed.Snapshot.PendingPrompt.Ask.PromptID

	// This is deliberately SIGKILL, not SIGTERM, Close, or
	// PrepareRuntimeShutdown. The process cannot run daemon cleanup hooks.
	firstServe.killAndRequireSIGKILL(t)
	firstAttach.waitAfterDaemonExit(t)

	secondServe := startExecutableRemoteServe(t, binary, processEnvironment, testRoot, "after-crash")
	defer secondServe.stop(t)
	waitExecutableRemoteEndpoint(t, endpoint, secondServe)
	secondAttach := startExecutableRemoteAttach(t, binary, processEnvironment, testRoot, "after-crash")
	defer secondAttach.close(t)
	secondInitialized := secondAttach.initialize(t, buildID, "desktop-crash-recovery-after")
	if secondInitialized.HostEpoch == "" || secondInitialized.HostEpoch == oldHostEpoch {
		t.Fatalf("Host epoch survived daemon SIGKILL: before=%q after=%q", oldHostEpoch, secondInitialized.HostEpoch)
	}
	var restored protocol.SessionSubscribeResult
	restoreResponse := secondAttach.request(t, protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: secondInitialized.HostEpoch, Target: created.Target, PageTurns: 60,
	})
	if restoreResponse.Error != nil {
		t.Fatalf("session/subscribe after SIGKILL error = %+v (daemon stderr=%s)", restoreResponse.Error, readProcessOutput(t, secondServe.stderr))
	}
	if err := json.Unmarshal(restoreResponse.Result, &restored); err != nil {
		t.Fatalf("decode session/subscribe after SIGKILL: %v (raw=%s)", err, restoreResponse.Result)
	}
	if restored.Snapshot.RuntimeEpoch == "" || restored.Snapshot.RuntimeEpoch == oldRuntimeEpoch {
		t.Fatalf("runtime epoch survived daemon SIGKILL: before=%q after=%q", oldRuntimeEpoch, restored.Snapshot.RuntimeEpoch)
	}
	if restored.Snapshot.PendingPrompt != nil || restored.Snapshot.Runtime.Running || restored.Snapshot.Runtime.CurrentTurn != nil {
		t.Fatalf("cold restart restored executable state: pending=%+v runtime=%+v", restored.Snapshot.PendingPrompt, restored.Snapshot.Runtime)
	}
	interruption := restored.Snapshot.Runtime.Interruption
	if restored.Snapshot.Runtime.LastOutcome != protocol.OutcomeInterrupted || interruption == nil ||
		!interruption.PreviousTurnInterrupted || interruption.Reason != protocol.InterruptionHostRestarted {
		t.Fatalf("cold restart interruption = %+v", restored.Snapshot.Runtime)
	}
	if len(restored.Snapshot.History.Messages) == 0 {
		t.Fatal("cold restart lost the accepted user Turn")
	}
	lastHistory := restored.Snapshot.History.Messages[len(restored.Snapshot.History.Messages)-1]
	if lastHistory.Role != "user" || lastHistory.Content == nil || !strings.Contains(*lastHistory.Content, "accepted before a real daemon SIGKILL") {
		t.Fatalf("cold restart history tail = %+v, want accepted user Turn", lastHistory)
	}

	staleHost := secondAttach.request(t, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "crash-old-host-cancel", ExpectedHostEpoch: oldHostEpoch,
			Target: created.Target, ExpectedRuntimeEpoch: oldRuntimeEpoch,
		},
		ExpectedTurnID: submitted.TurnID,
	})
	requireExecutableRemoteError(t, staleHost, protocol.ErrStaleHostEpoch)
	staleRuntime := secondAttach.request(t, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "crash-old-runtime-cancel", ExpectedHostEpoch: secondInitialized.HostEpoch,
			Target: created.Target, ExpectedRuntimeEpoch: oldRuntimeEpoch,
		},
		ExpectedTurnID: submitted.TurnID,
	})
	requireExecutableRemoteError(t, staleRuntime, protocol.ErrStaleRuntimeEpoch)
	staleTurn := secondAttach.request(t, protocol.MethodTurnCancel, protocol.TurnCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "crash-old-turn-cancel", ExpectedHostEpoch: secondInitialized.HostEpoch,
			Target: created.Target, ExpectedRuntimeEpoch: restored.Snapshot.RuntimeEpoch,
		},
		ExpectedTurnID: submitted.TurnID,
	})
	requireExecutableRemoteError(t, staleTurn, protocol.ErrTurnNotActive)
	stalePrompt := secondAttach.request(t, protocol.MethodPromptAnswer, protocol.PromptAnswerParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "crash-old-prompt-answer", ExpectedHostEpoch: secondInitialized.HostEpoch,
			Target: created.Target, ExpectedRuntimeEpoch: restored.Snapshot.RuntimeEpoch,
		},
		PromptID: oldPromptID,
		Answers:  []protocol.QuestionAnswer{{QuestionID: "q1", Selected: []string{"A"}}},
	})
	requireExecutableRemoteError(t, stalePrompt, protocol.ErrPromptNotPending)

	var detached protocol.DetachResult
	secondAttach.requestOK(t, protocol.MethodRemoteDetach, protocol.DetachParams{LeaseID: secondInitialized.Lease.LeaseID}, &detached)
	if !detached.Detached {
		t.Fatalf("remote/detach = %+v", detached)
	}
}

func runExecutableAttachExchange(t *testing.T, binary string, environment []string, buildID protocol.BuildID) protocol.HostEpoch {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stderr, err := os.CreateTemp(t.TempDir(), "attach-stderr-")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	attach := exec.CommandContext(ctx, binary, "remote", "attach", "--stdio")
	attach.Env = environment
	attach.Stderr = stderr
	stdin, err := attach.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := attach.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := attach.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = stdin.Close()
			_ = attach.Process.Kill()
			_ = attach.Wait()
		}
	}()

	encoder := json.NewEncoder(stdin)
	decoder := json.NewDecoder(stdout)
	if err := encoder.Encode(executableRPCRequest{
		JSONRPC: "2.0", ID: 1, Method: string(protocol.MethodRemoteInitialize),
		Params: protocol.InitializeParams{BuildID: buildID, ClientInstanceID: "desktop-executable-test"},
	}); err != nil {
		t.Fatal(err)
	}
	var initializeResponse executableRPCResponse
	if err := decoder.Decode(&initializeResponse); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if initializeResponse.Error != nil {
		t.Fatalf("initialize error = %+v", initializeResponse.Error)
	}
	var initialized protocol.InitializeResult
	if err := json.Unmarshal(initializeResponse.Result, &initialized); err != nil {
		t.Fatal(err)
	}
	if initializeResponse.ID != 1 || initialized.BuildID != buildID || initialized.Lease.LeaseID == "" {
		t.Fatalf("initialize response = %+v / %+v", initializeResponse, initialized)
	}

	if err := encoder.Encode(executableRPCRequest{
		JSONRPC: "2.0", ID: 2, Method: string(protocol.MethodRemoteDetach),
		Params: protocol.DetachParams{LeaseID: initialized.Lease.LeaseID},
	}); err != nil {
		t.Fatal(err)
	}
	var detachResponse executableRPCResponse
	if err := decoder.Decode(&detachResponse); err != nil {
		t.Fatalf("decode detach response: %v", err)
	}
	if detachResponse.Error != nil {
		t.Fatalf("detach error = %+v", detachResponse.Error)
	}
	var detached protocol.DetachResult
	if err := json.Unmarshal(detachResponse.Result, &detached); err != nil {
		t.Fatal(err)
	}
	if detachResponse.ID != 2 || !detached.Detached {
		t.Fatalf("detach response = %+v / %+v", detachResponse, detached)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	// Drain stdout before Wait. os/exec closes parent-side pipes as part of
	// Wait, which would turn the clean protocol EOF into os.ErrClosed and make
	// this assertion depend on scheduler ordering rather than attach behavior.
	var unexpected json.RawMessage
	if err := decoder.Decode(&unexpected); !errors.Is(err, io.EOF) {
		t.Fatalf("attach stdout contains an unexpected frame: %s (err=%v)", unexpected, err)
	}
	if err := attach.Wait(); err != nil {
		waited = true
		t.Fatalf("reasonix remote attach exit: %v (stderr=%s)", err, readProcessOutput(t, stderr))
	}
	waited = true
	if output := readProcessOutput(t, stderr); output != "" {
		t.Fatalf("reasonix remote attach wrote diagnostics on a successful exchange: %q", output)
	}
	return initialized.HostEpoch
}

type executableRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type executableRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

type executableServeProcess struct {
	command *exec.Cmd
	stdout  *os.File
	stderr  *os.File
	waited  bool
}

func startExecutableRemoteServe(t *testing.T, binary string, environment []string, outputDirectory, label string) *executableServeProcess {
	t.Helper()
	stdout, err := os.CreateTemp(outputDirectory, "serve-"+label+"-stdout-")
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(outputDirectory, "serve-"+label+"-stderr-")
	if err != nil {
		_ = stdout.Close()
		t.Fatal(err)
	}
	command := exec.Command(binary, "remote", "serve")
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatal(err)
	}
	process := &executableServeProcess{command: command, stdout: stdout, stderr: stderr}
	t.Cleanup(func() {
		if !process.waited {
			_ = process.command.Process.Kill()
			_ = process.command.Wait()
			process.waited = true
		}
		_ = process.stdout.Close()
		_ = process.stderr.Close()
	})
	return process
}

func waitExecutableRemoteEndpoint(t *testing.T, endpoint *service.Endpoint, process *executableServeProcess) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		probeContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		connection, err := endpoint.Dial(probeContext)
		cancel()
		if err == nil {
			_ = connection.Close()
			return
		}
		if time.Now().After(deadline) {
			waitErr := process.killAndWait()
			t.Fatalf("reasonix remote serve did not bind: %v (wait=%v, stderr=%s)", err, waitErr, readProcessOutput(t, process.stderr))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *executableServeProcess) killAndWait() error {
	if p == nil || p.waited {
		return nil
	}
	if p.command != nil && p.command.Process != nil {
		_ = p.command.Process.Kill()
	}
	err := p.command.Wait()
	p.waited = true
	return err
}

func (p *executableServeProcess) killAndRequireSIGKILL(t *testing.T) {
	t.Helper()
	if p == nil || p.command == nil || p.command.Process == nil || p.waited {
		t.Fatal("reasonix remote serve is not running")
	}
	if err := p.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err := p.command.Wait()
	p.waited = true
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("SIGKILL wait error = %v, want *exec.ExitError", err)
	}
	waitStatus, ok := exitError.Sys().(syscall.WaitStatus)
	if !ok || !waitStatus.Signaled() || waitStatus.Signal() != syscall.SIGKILL {
		t.Fatalf("reasonix remote serve exit status = %v, want SIGKILL", exitError.Sys())
	}
}

func (p *executableServeProcess) stop(t *testing.T) {
	t.Helper()
	if p == nil || p.waited {
		return
	}
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := p.command.Wait(); err != nil {
		p.waited = true
		t.Fatalf("reasonix remote serve exit: %v (stderr=%s)", err, readProcessOutput(t, p.stderr))
	}
	p.waited = true
	if output := readProcessOutput(t, p.stdout); output != "" {
		t.Fatalf("reasonix remote serve polluted stdout: %q", output)
	}
}

type executableAttachClient struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  *os.File
	encoder *json.Encoder
	decoder *json.Decoder
	nextID  int
	waited  bool
}

func startExecutableRemoteAttach(t *testing.T, binary string, environment []string, outputDirectory, label string) *executableAttachClient {
	t.Helper()
	contextValue, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	command := exec.CommandContext(contextValue, binary, "remote", "attach", "--stdio")
	command.Env = environment
	stderr, err := os.CreateTemp(outputDirectory, "attach-"+label+"-stderr-")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	command.Stderr = stderr
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		_ = stderr.Close()
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		_ = stderr.Close()
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatal(err)
	}
	client := &executableAttachClient{
		command: command, stdin: stdin, stdout: stdout, stderr: stderr,
		encoder: json.NewEncoder(stdin), decoder: json.NewDecoder(stdout),
	}
	t.Cleanup(func() {
		cancel()
		if !client.waited {
			_ = client.stdin.Close()
			_ = client.command.Process.Kill()
			_ = client.command.Wait()
			client.waited = true
		}
		_ = client.stdout.Close()
		_ = client.stderr.Close()
	})
	return client
}

func (c *executableAttachClient) initialize(t *testing.T, buildID protocol.BuildID, instance protocol.ClientInstanceID) protocol.InitializeResult {
	t.Helper()
	var initialized protocol.InitializeResult
	c.requestOK(t, protocol.MethodRemoteInitialize, protocol.InitializeParams{BuildID: buildID, ClientInstanceID: instance}, &initialized)
	if initialized.BuildID != buildID || initialized.HostEpoch == "" || initialized.Lease.LeaseID == "" {
		t.Fatalf("remote/initialize = %+v", initialized)
	}
	return initialized
}

func (c *executableAttachClient) requestOK(t *testing.T, method protocol.Method, params, destination any) {
	t.Helper()
	response := c.request(t, method, params)
	if response.Error != nil {
		t.Fatalf("%s error = %+v", method, response.Error)
	}
	if destination != nil {
		if err := json.Unmarshal(response.Result, destination); err != nil {
			t.Fatalf("decode %s result: %v (raw=%s)", method, err, response.Result)
		}
	}
}

func (c *executableAttachClient) request(t *testing.T, method protocol.Method, params any) executableRPCResponse {
	t.Helper()
	c.nextID++
	id := c.nextID
	if err := c.encoder.Encode(executableRPCRequest{JSONRPC: "2.0", ID: id, Method: string(method), Params: params}); err != nil {
		t.Fatalf("encode %s request: %v", method, err)
	}
	for {
		var raw json.RawMessage
		if err := c.decoder.Decode(&raw); err != nil {
			t.Fatalf("decode %s response: %v (attach stderr=%s)", method, err, readProcessOutput(t, c.stderr))
		}
		var identity struct {
			ID *int `json:"id"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			t.Fatalf("decode %s response identity: %v (raw=%s)", method, err, raw)
		}
		if identity.ID == nil {
			// Session notifications may legally race a later request. The
			// snapshot queries below are authoritative for this recovery test.
			continue
		}
		if *identity.ID != id {
			t.Fatalf("%s response id = %d, want %d (raw=%s)", method, *identity.ID, id, raw)
		}
		var response executableRPCResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatalf("decode %s response: %v (raw=%s)", method, err, raw)
		}
		return response
	}
}

func (c *executableAttachClient) waitAfterDaemonExit(t *testing.T) {
	t.Helper()
	if c == nil || c.waited {
		return
	}
	// The production attach closes its own stdin after observing Host EOF. Close
	// the parent writer as well so os/exec does not keep the SSH-side pipe open
	// while Wait verifies that the proxy terminated after the daemon crash.
	_ = c.stdin.Close()
	waited := make(chan error, 1)
	go func() { waited <- c.command.Wait() }()
	select {
	case <-waited:
		c.waited = true
	case <-time.After(10 * time.Second):
		t.Fatal("reasonix remote attach did not exit after daemon SIGKILL")
	}
}

func (c *executableAttachClient) close(t *testing.T) {
	t.Helper()
	if c == nil || c.waited {
		return
	}
	if err := c.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- c.command.Wait() }()
	select {
	case err := <-waited:
		c.waited = true
		if err != nil {
			t.Fatalf("reasonix remote attach exit: %v (stderr=%s)", err, readProcessOutput(t, c.stderr))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reasonix remote attach did not exit")
	}
}

func requireExecutableRemoteError(t *testing.T, response executableRPCResponse, code protocol.ReasonixErrorCode) {
	t.Helper()
	if response.Error == nil {
		t.Fatalf("request succeeded, want Remote error %s (result=%s)", code, response.Result)
	}
	var data protocol.RemoteErrorData
	if err := json.Unmarshal(response.Error.Data, &data); err != nil {
		t.Fatalf("decode Remote error data: %v (raw=%s)", err, response.Error.Data)
	}
	if data.ReasonixCode != code {
		t.Fatalf("Remote error = %+v, want %s", data, code)
	}
}

func readProcessOutput(t *testing.T, file *os.File) string {
	t.Helper()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
