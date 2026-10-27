package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

func TestMachineCommandsRejectInvalidProjectRoot(t *testing.T) {
	installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileWithStore := filepath.Join(dir, "with-store.txt")
	if err := os.WriteFile(fileWithStore, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.ProjectSessionDir(fileWithStore), 0o700); err != nil {
		t.Fatal(err)
	}
	missingWithFileStore := filepath.Join(dir, "file-store")
	storeFile := config.ProjectSessionDir(missingWithFileStore)
	if err := os.MkdirAll(filepath.Dir(storeFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storeFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range machineDirectoryCommands() {
		for _, path := range []struct {
			name    string
			value   string
			message string
		}{
			{"missing", filepath.Join(dir, "absent"), "--project-root does not exist"},
			{"file", file, "--project-root is not a directory"},
			{"file with store", fileWithStore, "--project-root is not a directory"},
			{"missing with file store", missingWithFileStore, "--project-root is not a directory"},
		} {
			t.Run(command.name+"/"+path.name, func(t *testing.T) {
				var out bytes.Buffer
				args := append(append([]string{}, command.args...), "--json", "--project-root", path.value)
				if code := command.run(args, &out); code != 2 {
					t.Fatalf("exit code = %d, want 2; output = %s", code, out.String())
				}
				var response machineErrorResponse
				if err := json.Unmarshal(out.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.SchemaVersion != machineSchemaVersion || response.Command != command.name ||
					response.Error.Code != "invalid_argument" || response.Error.Message != path.message {
					t.Fatalf("response = %+v", response)
				}
				if strings.Contains(out.String(), dir) {
					t.Fatalf("error output leaked path: %s", out.String())
				}
			})
		}
	}
}

func TestMachineCommandsAcceptProjectWithoutSessionStore(t *testing.T) {
	installMachineTestIdentity(t)
	projectRoot := testenv.TempDir(t)
	t.Chdir(projectRoot)
	sessionDir := config.ProjectSessionDir(projectRoot)
	for _, command := range machineDirectoryCommands() {
		for _, root := range []struct{ name, value string }{
			{"absolute", projectRoot},
			{"relative", "."},
			{"trimmed", " " + projectRoot + " "},
		} {
			t.Run(command.name+"/"+root.name, func(t *testing.T) {
				var out bytes.Buffer
				args := append(append([]string{}, command.args...), "--json", "--project-root", root.value)
				wantCode := 0
				if command.missing != "" {
					wantCode = 1
				}
				if code := command.run(args, &out); code != wantCode {
					t.Fatalf("exit code = %d, want %d; output = %s", code, wantCode, out.String())
				}
				var response struct {
					SchemaVersion int             `json:"schema_version"`
					Command       string          `json:"command"`
					Error         machineError    `json:"error"`
					Sessions      json.RawMessage `json:"sessions"`
					Recoveries    json.RawMessage `json:"recoveries"`
					Tasks         json.RawMessage `json:"tasks"`
				}
				if err := json.Unmarshal(out.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.SchemaVersion != machineSchemaVersion || response.Command != command.name || response.Error.Code != command.missing {
					t.Fatalf("response = %+v", response)
				}
				if command.missing == "" && string(response.Sessions) != "[]" && string(response.Recoveries) != "[]" && string(response.Tasks) != "[]" {
					t.Fatalf("expected an empty array: %s", out.String())
				}
			})
		}
	}
	if _, err := os.Stat(sessionDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("machine reads must leave the absent session store uncreated: %v", err)
	}
}

func TestMachineCommandsReadRetainedProjectStore(t *testing.T) {
	for _, transition := range []string{"deleted", "moved"} {
		t.Run(transition, func(t *testing.T) {
			identityKey := installMachineTestIdentity(t)
			projectRoot := filepath.Join(testenv.TempDir(t), "project")
			if err := os.Mkdir(projectRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			sessionDir := config.ProjectSessionDir(projectRoot)
			saveMachineTestSession(t, sessionDir, "session", time.Now().UTC())
			path := filepath.Join(sessionDir, "session.jsonl")
			sessionID := machineSessionIDWithKey("session", identityKey)
			if err := sessionstore.MarkSessionInFlightTurn(path, 1, true); err != nil {
				t.Fatal(err)
			}
			manager := jobs.NewManager(event.Discard)
			manager.SetActiveSessionPath("session", path)
			job := manager.StartForSession("session", "task", "PRIVATE TASK", func(context.Context, io.Writer) (string, error) {
				return "PRIVATE OUTPUT", nil
			})
			manager.WaitForSession(t.Context(), "session", []string{job.ID}, jobs.WaitOptions{Timeout: time.Second})
			manager.Close()
			var err error
			if transition == "deleted" {
				err = os.Remove(projectRoot)
			} else {
				err = os.Rename(projectRoot, projectRoot+"-moved")
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range machineDirectoryCommands() {
				t.Run(command.name, func(t *testing.T) {
					args := append([]string{}, command.args...)
					if len(args) > 1 {
						args[1] = sessionID
						if command.name == "task.show" {
							args[1] = job.ID
						}
					}
					args = append(args, "--json", "--project-root", projectRoot)
					var out bytes.Buffer
					if code := command.run(args, &out); code != 0 {
						t.Fatalf("exit code = %d, want 0; output = %s", code, out.String())
					}
					var response struct {
						SchemaVersion int               `json:"schema_version"`
						Command       string            `json:"command"`
						Sessions      []machineSession  `json:"sessions"`
						Session       machineSession    `json:"session"`
						Recoveries    []machineRecovery `json:"recoveries"`
						Tasks         []machineTask     `json:"tasks"`
						Task          machineTask       `json:"task"`
					}
					if err := json.Unmarshal(out.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if response.SchemaVersion != machineSchemaVersion || response.Command != command.name {
						t.Fatalf("response = %+v", response)
					}
					switch command.name {
					case "session.list":
						if len(response.Sessions) != 1 || response.Sessions[0].ID != sessionID {
							t.Fatalf("sessions = %+v", response.Sessions)
						}
					case "session.show", "session.status":
						if response.Session.ID != sessionID || response.Session.State != "interrupted" {
							t.Fatalf("session = %+v", response.Session)
						}
					case "session.recovery":
						if len(response.Recoveries) != 1 || response.Recoveries[0].SessionID != sessionID || !response.Recoveries[0].InFlight {
							t.Fatalf("recoveries = %+v", response.Recoveries)
						}
					case "task.list":
						if len(response.Tasks) != 1 || response.Tasks[0].ID != job.ID || response.Tasks[0].SessionID != sessionID {
							t.Fatalf("tasks = %+v", response.Tasks)
						}
					case "task.show":
						if response.Task.ID != job.ID || response.Task.SessionID != sessionID {
							t.Fatalf("task = %+v", response.Task)
						}
					}
					if strings.Contains(out.String(), projectRoot) || strings.Contains(out.String(), "PRIVATE") {
						t.Fatalf("response leaked private data: %s", out.String())
					}
				})
			}
		})
	}
}

func machineDirectoryCommands() []struct {
	name    string
	run     func([]string, io.Writer) int
	args    []string
	missing string
} {
	return []struct {
		name    string
		run     func([]string, io.Writer) int
		args    []string
		missing string
	}{
		{"session.list", runSessionCommand, []string{"list"}, ""},
		{"session.show", runSessionCommand, []string{"show", "missing"}, "session_not_found"},
		{"session.status", runSessionCommand, []string{"status", "missing"}, "session_not_found"},
		{"session.recovery", runSessionCommand, []string{"recovery"}, ""},
		{"task.list", runTaskCommand, []string{"list"}, ""},
		{"task.show", runTaskCommand, []string{"show", "missing"}, "task_not_found"},
	}
}
