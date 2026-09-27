package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	contractPrivatePrompt = "HOST_PRIVATE_INSTRUCTIONS_café_5905\nKeep this standing guidance private.\n"
	contractConfigured    = "CONFIGURED_STANDING_INSTRUCTIONS_5905"
	contractMemory        = "PROJECT_STANDING_INSTRUCTIONS_5905"
)

type contractRequest struct {
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

type contractCLI struct {
	binary, home, workspace, promptFile string
	env                                 []string
	requests                            chan contractRequest
}

func TestAppendSystemPromptFileContract(t *testing.T) {
	binary := buildContractCLI(t)
	t.Run("run and exact resume", func(t *testing.T) {
		cli := newContractCLI(t, binary)
		args := []string{"run", "--events-jsonl", "--append-system-prompt-file", cli.promptFile}
		output := cli.run(t, 0, append(args, "first public task")...)
		assertContractRunDone(t, output)
		firstID := cli.storedSessionID(t)
		assertContractRequest(t, cli.request(t), contractPrivatePrompt, "first public task")
		updatedPrompt := contractPrivatePrompt + "Updated standing guidance on resume.\n"
		writeContractFile(t, cli.promptFile, []byte(updatedPrompt))
		output = cli.run(t, 0, append(args, "--resume", firstID, "second public task")...)
		assertContractRunDone(t, output)
		if got := cli.storedSessionID(t); got != firstID {
			t.Fatalf("resume changed canonical session identity: %q != %q", got, firstID)
		}
		assertContractRequest(t, cli.request(t), updatedPrompt, "first public task", "second public task")
		cli.run(t, 0, "run", "--events-jsonl", "third public task")
		assertContractRequest(t, cli.request(t), "", "third public task")
		cli.run(t, 0, "doctor", "--json")
		cli.assertPrivateDiagnostics(t)
	})
	t.Run("interactive terminal", func(t *testing.T) {
		cli := newContractCLI(t, binary)
		runAppendPromptTUI(t, cli)
		cli.assertPrivateDiagnostics(t)
	})
	t.Run("relative path resolves after dir", func(t *testing.T) {
		cli := newContractCLI(t, binary)
		launchDir := t.TempDir()
		relativePath := filepath.Base(cli.promptFile)
		writeContractFile(t, filepath.Join(launchDir, relativePath), []byte("incorrect launch-directory instructions"))
		output := cli.runInDir(t, launchDir, 0, "run", "--events-jsonl", "--dir", cli.workspace,
			"--append-system-prompt-file", relativePath, "relative path task")
		assertContractRunDone(t, output)
		assertContractPrivate(t, output, relativePath)
		assertContractRequest(t, cli.request(t), contractPrivatePrompt, "relative path task")
	})
	t.Run("terminator preserves flag-like user text", func(t *testing.T) {
		cli := newContractCLI(t, binary)
		output := cli.run(t, 0, "run", "--events-jsonl", "--", "--append-system-prompt-file", "nonexistent-host-file.txt")
		assertContractRunDone(t, output)
		assertContractRequest(t, cli.request(t), "", "--append-system-prompt-file nonexistent-host-file.txt")
	})
	t.Run("unreadable file fails before provider use", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("unreadable-file contract requires Unix permissions and a non-root user")
		}
		cli := newContractCLI(t, binary)
		if err := os.Chmod(cli.promptFile, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(cli.promptFile, 0o600) })
		if _, err := os.ReadFile(cli.promptFile); !os.IsPermission(err) {
			t.Skip("filesystem does not deny reads from a mode-000 file")
		}
		for _, command := range [][]string{{"run"}, {}} {
			args := append(append([]string{}, command...), "--append-system-prompt-file", cli.promptFile)
			cli.run(t, 2, append(args, "public task")...)
		}
		select {
		case <-cli.requests:
			t.Fatal("unreadable prompt file reached the provider")
		default:
		}
	})
	t.Run("invalid files fail before provider use", func(t *testing.T) {
		cli := newContractCLI(t, binary)
		for name, body := range map[string][]byte{"empty": {}, "whitespace": []byte(" \n\t"), "invalid-utf8": append([]byte(contractPrivatePrompt), 0xff)} {
			writeContractFile(t, filepath.Join(cli.workspace, name), body)
		}
		for _, command := range [][]string{{"run"}, {}} {
			for _, name := range []string{"missing", "empty", "whitespace", "invalid-utf8", "."} {
				path := filepath.Join(cli.workspace, name)
				args := append(append([]string{}, command...), "--append-system-prompt-file", path)
				output := cli.run(t, 2, append(args, "public task")...)
				assertContractPrivate(t, output, path)
			}
			cli.run(t, 2, append(command, "--append-system-prompt-file")...)
		}
		select {
		case <-cli.requests:
			t.Fatal("invalid prompt file reached the provider")
		default:
		}
	})
	t.Run("public help", func(t *testing.T) {
		cli := newContractCLI(t, binary)
		for _, args := range [][]string{{"--help"}, {"run", "--help"}} {
			if output := cli.run(t, 0, args...); !strings.Contains(output, "--append-system-prompt-file") {
				t.Fatalf("help does not advertise the flag: %s", output)
			}
		}
	})
}

func buildContractCLI(t *testing.T) string {
	t.Helper()
	// A release binary can run the same contract without rebuilding, allowing
	// hosts to qualify the exact artifact they will install.
	if binary := os.Getenv("REASONIX_TEST_BINARY"); binary != "" {
		return binary
	}
	name := "reasonix"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return binary
}

func newContractCLI(t *testing.T, binary string) *contractCLI {
	t.Helper()
	cli := &contractCLI{
		binary: binary, home: t.TempDir(), workspace: t.TempDir(),
		requests: make(chan contractRequest, 16),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request contractRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode provider request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		cli.requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"contract response\"}}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	body := fmt.Sprintf(`default_model = "fake/model-a"
language = "en"
[telemetry]
cli_metrics = "off"
[agent]
system_prompt = %q
[[providers]]
name = "fake"
kind = "openai"
base_url = %q
models = ["model-a"]
default = "model-a"
api_key_env = "REASONIX_CONTRACT_KEY"
`, contractConfigured, server.URL+"/v1")
	writeContractFile(t, filepath.Join(cli.home, "config.toml"), []byte(body))
	writeContractFile(t, filepath.Join(cli.workspace, "AGENTS.md"), []byte(contractMemory))
	cli.promptFile = filepath.Join(cli.workspace, "private-host-instructions-5905.txt")
	writeContractFile(t, cli.promptFile, []byte(contractPrivatePrompt))
	// Do not inherit credentials, proxy settings, or production configuration.
	cli.env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + cli.home,
		"USERPROFILE=" + cli.home, "REASONIX_HOME=" + cli.home,
		"REASONIX_CONTRACT_KEY=local-test-key", "REASONIX_LANG=en", "TERM=xterm-256color"}
	for _, key := range []string{"SYSTEMROOT", "TMPDIR", "TEMP", "TMP"} {
		if value := os.Getenv(key); value != "" {
			cli.env = append(cli.env, key+"="+value)
		}
	}
	// Normal CLI startup may migrate the fixture config. Establish that baseline
	// before checking that the host option itself leaves standing files alone.
	cli.run(t, 0, "run", "--help")
	baseline, err := os.ReadFile(filepath.Join(cli.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for path, want := range map[string]string{
			filepath.Join(cli.home, "config.toml"): string(baseline), filepath.Join(cli.workspace, "AGENTS.md"): contractMemory,
		} {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want {
				t.Errorf("host option changed %s: %v", filepath.Base(path), err)
			}
		}
	})
	return cli
}

func writeContractFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (cli *contractCLI) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, cli.binary, args...)
	cmd.Dir, cmd.Env = cli.workspace, cli.env
	return cmd
}

func (cli *contractCLI) run(t *testing.T, wantCode int, args ...string) string {
	t.Helper()
	return cli.runInDir(t, cli.workspace, wantCode, args...)
}

func (cli *contractCLI) runInDir(t *testing.T, dir string, wantCode int, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cmd := cli.command(ctx, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	code := 0
	if err != nil {
		if cmd.ProcessState == nil {
			t.Fatal(err)
		}
		code = cmd.ProcessState.ExitCode()
	}
	if code != wantCode {
		t.Fatalf("CLI exit = %d, want %d: %s%s", code, wantCode, output, stderr.String())
	}
	assertContractPrivate(t, string(output), cli.promptFile)
	assertContractPrivate(t, stderr.String(), cli.promptFile)
	return string(output)
}

func (cli *contractCLI) request(t *testing.T) contractRequest {
	t.Helper()
	select {
	case request := <-cli.requests:
		return request
	case <-time.After(time.Minute):
		t.Fatal("CLI did not send a provider request")
		return contractRequest{}
	}
}

func assertContractRequest(t *testing.T, request contractRequest, appended string, tasks ...string) {
	t.Helper()
	var systemBuilder strings.Builder
	var users []string
	for _, message := range request.Messages {
		var content string
		if err := json.Unmarshal(message.Content, &content); err != nil {
			t.Fatalf("message content is not text: %v", err)
		}
		if message.Role == "system" {
			systemBuilder.WriteString(content)
		} else if strings.Contains(content, "HOST_PRIVATE_INSTRUCTIONS") {
			t.Fatalf("host instructions leaked to %s role", message.Role)
		}
		if message.Role == "user" {
			users = append(users, content)
		}
	}
	system := systemBuilder.String()
	wantCount := 0
	if appended != "" {
		wantCount = 1
	}
	if strings.Count(system, "HOST_PRIVATE_INSTRUCTIONS") != wantCount || (appended != "" && !strings.Contains(system, appended)) {
		t.Fatal("provider system prompt did not contain the exact expected host block count")
	}
	configured, memory := strings.Index(system, contractConfigured), strings.Index(system, contractMemory)
	if configured < 0 || memory <= configured || (appended != "" && strings.Index(system, appended) <= memory) {
		t.Fatal("provider system prompt lost configured/project guidance or composition order")
	}
	for _, task := range tasks {
		if !strings.Contains(strings.Join(users, "\n"), task) {
			t.Fatalf("user history is missing %q", task)
		}
	}
}

func assertContractRunDone(t *testing.T, output string) {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		var record struct {
			Kind string
			OK   bool `json:"ok"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("events-jsonl contains a non-JSON line: %s", line)
		}
		if record.Kind == "run_done" && record.OK {
			return
		}
	}
	t.Fatalf("events-jsonl has no successful result: %s", output)
}

func (cli *contractCLI) storedSessionID(t *testing.T) string {
	t.Helper()
	// Native sessions currently omit session_id from run_done. Read only the
	// fixture's public manifest to select the exact persisted session to resume.
	var ids []string
	err := filepath.WalkDir(cli.home, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "manifest.json" {
			return err
		}
		if filepath.Base(filepath.Dir(filepath.Dir(path))) != "sessions-v4" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var manifest struct {
			ID string `json:"sessionId"`
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			return err
		}
		ids = append(ids, manifest.ID)
		return nil
	})
	if err != nil || len(ids) != 1 || ids[0] == "" {
		t.Fatalf("expected exactly one stored session, got %q: %v", ids, err)
	}
	return ids[0]
}

func assertContractPrivate(t *testing.T, output, path string) {
	t.Helper()
	if strings.Contains(output, "HOST_PRIVATE_INSTRUCTIONS") || strings.Contains(output, path) {
		t.Fatal("host instructions or their path leaked into CLI output/diagnostics")
	}
}

func (cli *contractCLI) assertPrivateDiagnostics(t *testing.T) {
	t.Helper()
	err := filepath.WalkDir(cli.home, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if strings.HasSuffix(path, ".log") || entry.Name() == "config.toml" {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			assertContractPrivate(t, string(body), cli.promptFile)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
