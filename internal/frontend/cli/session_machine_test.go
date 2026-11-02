package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func TestSessionMachineListIsStableAndRedacted(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
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

func TestMachineProjectSessionDirUsesProjectStore(t *testing.T) {
	projectRoot := testenv.TempDir(t)
	got := machineProjectSessionDir(projectRoot)
	want := config.ProjectSessionDir(projectRoot)
	if got != want {
		t.Fatalf("machine project session dir = %q, want %q", got, want)
	}
	if got == projectRoot {
		t.Fatalf("machine project session dir must not use the project root directly: %q", got)
	}
}

func TestSessionMachineProjectRootUsesProjectStore(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	projectRoot := testenv.TempDir(t)
	sessionDir := config.ProjectSessionDir(projectRoot)
	saveMachineTestSession(t, sessionDir, "project", time.Date(2026, 7, 23, 11, 30, 0, 0, time.UTC))

	var out bytes.Buffer
	if code := runSessionCommand([]string{"list", "--json", "--project-root", projectRoot}, &out); code != 0 {
		t.Fatalf("list exit code = %d, output = %s", code, out.String())
	}
	var response machineSessionList
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(response.Sessions) != 1 || response.Sessions[0].ID != machineSessionIDWithKey("project", identityKey) {
		t.Fatalf("sessions = %+v, want project session", response.Sessions)
	}
}

func TestSessionMachineShowAndStatusExposeOnlySafeState(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	saveMachineTestSession(t, dir, "busy", time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	path := filepath.Join(dir, "busy.jsonl")
	lease, err := sessionstore.TryAcquireSessionLease(path)
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
	dir := testenv.TempDir(t)
	file := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		code int
		err  string
	}{
		{name: "missing json", args: []string{"list", "--dir", dir}, code: 2, err: "invalid_argument"},
		{name: "missing session", args: []string{"show", "--json", "missing", "--dir", dir}, code: 1, err: "session_not_found"},
		{name: "dir that does not exist", args: []string{"list", "--json", "--dir", filepath.Join(dir, "absent")}, code: 2, err: "invalid_argument"},
		{name: "dir that is a file", args: []string{"list", "--json", "--dir", file}, code: 2, err: "invalid_argument"},
		{name: "recovery in a dir that does not exist", args: []string{"recovery", "--json", "--dir", filepath.Join(dir, "absent")}, code: 2, err: "invalid_argument"},
		{name: "conflicting session sources", args: []string{"list", "--json", "--dir", dir, "--project-root", dir}, code: 2, err: "invalid_argument"},
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
	session := sessionstore.NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "private prompt"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "private answer"})
	if err := session.Save(path); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if err := sessionstore.SaveBranchMetaPreserveUpdated(path, sessionstore.BranchMeta{
		ID:            id,
		CreatedAt:     updatedAt.Add(-time.Hour),
		UpdatedAt:     updatedAt,
		Scope:         "project",
		SchemaVersion: sessionstore.BranchMetaCountsVersion,
		Turns:         1,
		Preview:       "PRIVATE prompt",
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
	if got, _, _ := sessionstore.LoadBranchMeta(path); !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("saved meta says updated %v, the fixture asked for %v: ordering would follow the write clock", got.UpdatedAt, updatedAt)
	}
}

// installUnprovableMachineLease leaves a readable foreign sidecar and a lease
// lock the probe cannot open, so ownership is neither provably held nor free.
func installUnprovableMachineLease(t *testing.T, sessionPath string) {
	t.Helper()
	if err := sessionstore.SaveSessionLeaseInfo(sessionPath, sessionstore.SessionLeaseInfo{
		SessionPath: sessionPath,
		WriterID:    "other-host-1234-deadbeef",
		PID:         os.Getpid() + 1,
		AcquiredAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save foreign lease info: %v", err)
	}
	if err := os.Mkdir(store.SessionLeaseLock(sessionstore.CanonicalSessionPath(sessionPath)), 0o755); err != nil {
		t.Fatalf("make unopenable lease lock: %v", err)
	}
}

// An unprovable lease supports no state claim: neither active (a holder was
// never observed) nor idle/interrupted/recovered (nobody proved it free).
func TestSessionMachineSaysUnknownWhenLeaseOwnershipIsUnprovable(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := testenv.TempDir(t)
	saveMachineTestSession(t, dir, "unknown", time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	installUnprovableMachineLease(t, filepath.Join(dir, "unknown.jsonl"))

	var listOut bytes.Buffer
	if code := runSessionCommand([]string{"list", "--json", "--dir", dir}, &listOut); code != 0 {
		t.Fatalf("list exit code = %d, output = %s", code, listOut.String())
	}
	var list machineSessionList
	if err := json.Unmarshal(listOut.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].State != machineStateUnknown {
		t.Fatalf("sessions = %+v, want one session in state %q", list.Sessions, machineStateUnknown)
	}

	var showOut bytes.Buffer
	args := []string{"status", "--json", machineSessionIDWithKey("unknown", identityKey), "--dir", dir}
	if code := runSessionCommand(args, &showOut); code != 0 {
		t.Fatalf("status exit code = %d, output = %s", code, showOut.String())
	}
	var show machineSessionShow
	if err := json.Unmarshal(showOut.Bytes(), &show); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if show.Session.State != machineStateUnknown {
		t.Fatalf("status = %+v, want state %q", show.Session, machineStateUnknown)
	}
}
