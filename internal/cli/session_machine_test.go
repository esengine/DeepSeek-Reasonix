package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestSessionMachineListIsStableAndRedacted(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := t.TempDir()
	saveMachineTestSession(t, dir, "older", time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC))
	saveMachineTestSession(t, dir, "newer", time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC))

	var out bytes.Buffer
	if code := runSessionCommand([]string{"list", "--dir", dir, "--json"}, &out); code != 0 {
		t.Fatalf("list exit code = %d, output = %s", code, out.String())
	}
	var response machineSessionList
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if response.SchemaVersion != machineSchemaVersion || response.Command != "session.list" {
		t.Fatalf("header = %+v", response)
	}
	if len(response.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want two sessions", response.Sessions)
	}
	if got := response.Sessions[0].ID; got != machineSessionIDWithKey("newer", identityKey) {
		t.Errorf("first session = %q, want opaque newer id", got)
	}
	if got := response.Sessions[1].ID; got != machineSessionIDWithKey("older", identityKey) {
		t.Errorf("second session = %q, want opaque older id", got)
	}
	if response.Sessions[0].Turns != 1 || response.Sessions[0].Scope != "project" {
		t.Errorf("session metadata = %+v", response.Sessions[0])
	}
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), dir) {
		t.Fatalf("machine output leaked private content or path: %s", out.String())
	}
}

func TestSessionMachineShowAndStatusExposeOnlySafeState(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := t.TempDir()
	saveMachineTestSession(t, dir, "busy", time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	path := filepath.Join(dir, "busy.jsonl")
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("acquire session lease: %v", err)
	}
	defer lease.Release()

	for _, operation := range []string{"show", "status"} {
		var out bytes.Buffer
		args := []string{operation, "--json", machineSessionIDWithKey("busy", identityKey), "--dir", dir}
		if code := runSessionCommand(args, &out); code != 0 {
			t.Fatalf("%s exit code = %d, output = %s", operation, code, out.String())
		}
		var response machineSessionShow
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatalf("decode %s: %v", operation, err)
		}
		if response.Command != "session."+operation || response.Session.State != "active" {
			t.Errorf("%s response = %+v", operation, response)
		}
		if strings.Contains(out.String(), dir) || strings.Contains(out.String(), "PRIVATE") {
			t.Fatalf("%s output leaked private data: %s", operation, out.String())
		}
	}
}

func TestMachineSessionIDIsStableAndOpaque(t *testing.T) {
	raw := "20260723-120000.000000000-private-provider-model"
	identityKey := bytes.Repeat([]byte{0x41}, machineIdentityKeyBytes)
	otherKey := bytes.Repeat([]byte{0x42}, machineIdentityKeyBytes)
	first := machineSessionIDWithKey(raw, identityKey)
	if first == "" || first != machineSessionIDWithKey(raw, identityKey) {
		t.Fatalf("machine session id is not stable: %q", first)
	}
	if strings.Contains(first, "private-provider-model") || first == raw {
		t.Fatalf("machine session id exposed raw branch identity: %q", first)
	}
	if first == machineSessionIDWithKey(raw+"-other", identityKey) {
		t.Fatalf("different branch ids collided: %q", first)
	}
	if first == machineSessionIDWithKey(raw, otherKey) {
		t.Fatalf("different installations produced the same machine id: %q", first)
	}
}

func TestSessionMachineErrorsAreJSONAndNonZero(t *testing.T) {
	installMachineTestIdentity(t)
	dir := t.TempDir()
	cases := []struct {
		name string
		args []string
		code int
		err  string
	}{
		{name: "missing json", args: []string{"list", "--dir", dir}, code: 2, err: "invalid_argument"},
		{name: "missing session", args: []string{"show", "--json", "missing", "--dir", dir}, code: 1, err: "session_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := runSessionCommand(tc.args, &out); code != tc.code {
				t.Fatalf("exit code = %d, want %d; output = %s", code, tc.code, out.String())
			}
			var response machineErrorResponse
			if err := json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if response.SchemaVersion != machineSchemaVersion || response.Error.Code != tc.err {
				t.Fatalf("response = %+v", response)
			}
			if strings.Contains(out.String(), dir) {
				t.Fatalf("error output leaked path: %s", out.String())
			}
		})
	}
}

func saveMachineTestSession(t *testing.T, dir, id string, updatedAt time.Time) {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	session := agent.NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "private prompt"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "private answer"})
	if err := session.Save(path); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		ID:            id,
		CreatedAt:     updatedAt.Add(-time.Hour),
		UpdatedAt:     updatedAt,
		Scope:         "project",
		SchemaVersion: agent.BranchMetaCountsVersion,
		Turns:         1,
		Preview:       "PRIVATE prompt",
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
}
