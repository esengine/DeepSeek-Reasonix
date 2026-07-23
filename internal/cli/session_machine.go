package cli

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/recovery"
	"reasonix/internal/store"
)

const (
	machineSchemaVersion    = 1
	machineIdentityKeyBytes = 32
	machineIdentityKeyFile  = "machine-id.key"
)

type machineSession struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Scope     string `json:"scope"`
	Turns     int    `json:"turns"`
	State     string `json:"state"`
	Recovered bool   `json:"recovered"`
}

type machineSessionList struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Sessions      []machineSession `json:"sessions"`
}

type machineSessionShow struct {
	SchemaVersion int            `json:"schema_version"`
	Command       string         `json:"command"`
	Session       machineSession `json:"session"`
}

type machineRecovery struct {
	SessionID string `json:"session_id"`
	State     string `json:"state"`
	UpdatedAt string `json:"updated_at"`
	Tasks     int    `json:"tasks"`
	Failures  int    `json:"failures"`
	Pending   int    `json:"pending"`
	InFlight  bool   `json:"in_flight"`
}

type machineRecoveryList struct {
	SchemaVersion int               `json:"schema_version"`
	Command       string            `json:"command"`
	Recoveries    []machineRecovery `json:"recoveries"`
}

type machineError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type machineErrorResponse struct {
	SchemaVersion int          `json:"schema_version"`
	Command       string       `json:"command"`
	Error         machineError `json:"error"`
}

type sessionMachineOptions struct {
	dir    string
	target string
	json   bool
}

func sessionCommand(args []string) int {
	return runSessionCommand(args, os.Stdout)
}

func runSessionCommand(args []string, out io.Writer) int {
	command := "session"
	if len(args) == 0 {
		return writeMachineError(out, command, "invalid_argument", "a session operation is required")
	}
	operation := args[0]
	command = "session." + operation
	if operation != "list" && operation != "show" && operation != "status" && operation != "recovery" {
		return writeMachineError(out, command, "unknown_command", "unknown session operation")
	}
	options, code, message := parseSessionMachineOptions(args[1:], operation)
	if code != "" {
		return writeMachineError(out, command, code, message)
	}
	if !options.json {
		return writeMachineError(out, command, "invalid_argument", "--json is required")
	}
	if options.dir == "" {
		options.dir = resolveCLISessionDir()
	}
	identityKey, err := loadMachineIdentityKey()
	if err != nil {
		return writeMachineError(out, command, "machine_identity_unavailable", "machine identity is unavailable")
	}
	if operation == "recovery" {
		recoveries, err := machineRecoveries(options.dir, options.target, identityKey)
		if err != nil {
			return writeMachineError(out, command, "recovery_state_unavailable", "recovery state is unavailable")
		}
		return writeMachineJSON(out, machineRecoveryList{SchemaVersion: machineSchemaVersion, Command: command, Recoveries: recoveries})
	}
	sessions, err := machineSessions(options.dir, identityKey)
	if err != nil {
		return writeMachineError(out, command, "session_dir_unavailable", "session directory is unavailable")
	}
	if operation == "list" {
		return writeMachineJSON(out, machineSessionList{
			SchemaVersion: machineSchemaVersion,
			Command:       command,
			Sessions:      sessions,
		})
	}
	for _, session := range sessions {
		if session.ID != options.target {
			continue
		}
		return writeMachineJSON(out, machineSessionShow{
			SchemaVersion: machineSchemaVersion,
			Command:       command,
			Session:       session,
		})
	}
	return writeMachineError(out, command, "session_not_found", "session was not found")
}

func parseSessionMachineOptions(args []string, operation string) (sessionMachineOptions, string, string) {
	var options sessionMachineOptions
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			options.json = true
		case "--dir":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return options, "invalid_argument", "--dir requires a value"
			}
			i++
			options.dir = args[i]
		case "--help", "-h":
			return options, "invalid_argument", "use the documented machine interface"
		default:
			arg := strings.TrimSpace(args[i])
			if strings.HasPrefix(arg, "-") {
				return options, "invalid_argument", "unknown session option"
			}
			if operation == "list" || options.target != "" || strings.ContainsAny(arg, `/\\`) {
				return options, "invalid_argument", "invalid session identifier"
			}
			options.target = arg
		}
	}
	if operation != "list" && operation != "recovery" && options.target == "" {
		return options, "invalid_argument", "a session identifier is required"
	}
	return options, "", ""
}

func machineRecoveries(dir, target string, identityKey []byte) ([]machineRecovery, error) {
	ordered, err := agent.ListSessionOrder(dir)
	if err != nil {
		return nil, err
	}
	out := make([]machineRecovery, 0, len(ordered))
	for _, info := range ordered {
		sessionID := machineSessionIDWithKey(agent.BranchID(info.Path), identityKey)
		if target != "" && sessionID != target {
			continue
		}
		meta, metaOK, _ := agent.LoadBranchMeta(info.Path)
		snapshot, err := recovery.LoadSnapshot(info.Path)
		if err != nil {
			return nil, err
		}
		if len(snapshot.Tasks) == 0 && (meta.InFlightTurn == nil || !metaOK) {
			continue
		}
		item := machineRecovery{
			SessionID: sessionID,
			UpdatedAt: machineTime(info.LastActivityAt),
			InFlight:  metaOK && meta.InFlightTurn != nil,
		}
		for _, task := range snapshot.Tasks {
			if task == nil {
				continue
			}
			item.Tasks++
			if task.Failure != nil {
				item.Failures++
			}
			if task.Pending != nil {
				item.Pending++
			}
		}
		switch {
		case item.Pending > 0:
			item.State = string(recovery.PhaseAwaitingDecision)
		case item.Failures > 0:
			item.State = "failed"
		case item.InFlight:
			item.State = "interrupted"
		default:
			item.State = string(recovery.PhaseIdle)
		}
		if stat, statErr := os.Stat(store.SessionRecoveryState(info.Path)); statErr == nil && stat.ModTime().After(parseMachineTime(item.UpdatedAt)) {
			item.UpdatedAt = machineTime(stat.ModTime())
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].SessionID < out[j].SessionID
	})
	if target != "" && len(out) == 0 {
		return nil, os.ErrNotExist
	}
	return out, nil
}

func parseMachineTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func machineSessions(dir string, identityKey []byte) ([]machineSession, error) {
	ordered, err := agent.ListSessionOrder(dir)
	if err != nil {
		return nil, err
	}
	out := make([]machineSession, 0, len(ordered))
	for _, info := range ordered {
		turns := info.Turns
		if info.SchemaVersion < agent.BranchMetaCountsVersion {
			_, turns = agent.SessionPreview(info.Path)
		}
		if turns == 0 {
			continue
		}
		meta, ok, _ := agent.LoadBranchMeta(info.Path)
		state := "idle"
		if agent.SessionLeaseHeld(info.Path) {
			state = "active"
		} else if ok && meta.InFlightTurn != nil {
			state = "interrupted"
		} else if info.Recovered {
			state = "recovered"
		}
		scope := info.Scope
		if scope == "" {
			scope = "global"
		}
		out = append(out, machineSession{
			ID:        machineSessionIDWithKey(agent.BranchID(info.Path), identityKey),
			CreatedAt: machineTime(info.CreatedAt),
			UpdatedAt: machineTime(info.LastActivityAt),
			Scope:     scope,
			Turns:     turns,
			State:     state,
			Recovered: info.Recovered,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

func machineTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func loadMachineIdentityKey() ([]byte, error) {
	root := strings.TrimSpace(config.MemoryUserDir())
	if root == "" {
		return nil, fmt.Errorf("machine identity: Reasonix state directory is unavailable")
	}
	path := filepath.Join(root, machineIdentityKeyFile)
	key, err := readMachineIdentityKey(path)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("machine identity: create state directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return nil, fmt.Errorf("machine identity: initialize key: %w", err)
	}
	defer unlock()

	key, err = readMachineIdentityKey(path)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key = make([]byte, machineIdentityKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("machine identity: generate key: %w", err)
	}
	if err := fileutil.AtomicWriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("machine identity: persist key: %w", err)
	}
	return key, nil
}

func readMachineIdentityKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != machineIdentityKeyBytes {
		return nil, fmt.Errorf("machine identity: invalid key length %d", len(key))
	}
	return key, nil
}

// machineSessionIDWithKey keeps the public machine contract stable without
// exposing or making offline guesses about transcript filenames, which include
// creation timestamps and configured model labels.
func machineSessionIDWithKey(branchID string, identityKey []byte) string {
	branchID = strings.TrimSpace(branchID)
	if branchID == "" || len(identityKey) != machineIdentityKeyBytes {
		return ""
	}
	digest := hmac.New(sha256.New, identityKey)
	_, _ = digest.Write([]byte("reasonix-machine-session-v1\x00"))
	_, _ = digest.Write([]byte(branchID))
	return "session_" + hex.EncodeToString(digest.Sum(nil)[:16])
}

func writeMachineJSON(out io.Writer, value any) int {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return 1
	}
	return 0
}

func writeMachineError(out io.Writer, command, code, message string) int {
	if writeMachineJSON(out, machineErrorResponse{
		SchemaVersion: machineSchemaVersion,
		Command:       command,
		Error: machineError{
			Code:    code,
			Message: message,
		},
	}) != 0 {
		return 1
	}
	if code == "invalid_argument" || code == "unknown_command" {
		return 2
	}
	return 1
}
