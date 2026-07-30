package protocol

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestFrozenErrorTableHasExactly51ControlledEntries(t *testing.T) {
	contracts := ErrorContracts()
	if len(contracts) != 51 {
		t.Fatalf("error contracts = %d, want 51", len(contracts))
	}
	seen := map[ReasonixErrorCode]bool{}
	gotCodes := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		gotCodes = append(gotCodes, string(contract.ReasonixCode))
		if seen[contract.ReasonixCode] {
			t.Fatalf("duplicate error %s", contract.ReasonixCode)
		}
		seen[contract.ReasonixCode] = true
		if contract.JSONRPCCode != DomainErrorCode || strings.TrimSpace(contract.Message) == "" {
			t.Fatalf("invalid contract %+v", contract)
		}
		options := ErrorOptions{}
		if contract.ReasonixCode == ErrHostBusy {
			retry := int64(100)
			options.RetryAfterMs = &retry
		}
		if contract.ReasonixCode == ErrRewindPartial {
			workspace, conversation, snapshot := true, false, true
			options.WorkspaceMayHaveChanged = &workspace
			options.ConversationMayHaveChanged = &conversation
			options.SnapshotRequired = &snapshot
		}
		remoteErr, err := NewRemoteError(contract.ReasonixCode, options)
		if err != nil {
			t.Fatalf("construct %s: %v", contract.ReasonixCode, err)
		}
		if remoteErr.Message != contract.Message || remoteErr.RPCError().Code != DomainErrorCode {
			t.Fatalf("constructed error drift for %s", contract.ReasonixCode)
		}
	}
	wantCodes := strings.Fields(`
REMOTE_NOT_INSTALLED HOST_STOPPED VERSION_MISMATCH DAEMON_RESTART_REQUIRED HOST_BUSY
STALE_HOST_EPOCH STALE_RUNTIME_EPOCH REQUEST_ID_CONFLICT LEASE_NOT_HELD STALE_CONNECTION
STALE_DIRECTORY_REF DIRECTORY_NOT_FOUND NOT_DIRECTORY PERMISSION_DENIED WORKSPACE_NOT_FOUND
WORKSPACE_IN_USE SESSION_NOT_FOUND WORKSPACE_SESSION_MISMATCH RUNTIME_START_FAILED SESSION_PERSIST_FAILED
SESSION_TRASHED SESSION_BUSY SESSION_CLEANUP_PENDING TOPIC_NOT_FOUND TOPIC_NOT_EMPTY
TRASH_ENTRY_NOT_FOUND RECOVERY_GUARD_FAILED INVALID_PROFILE MODEL_NOT_AVAILABLE EFFORT_NOT_SUPPORTED
TURN_ALREADY_RUNNING TURN_NOT_ACTIVE TURN_MISMATCH OPERATION_NOT_ACTIVE OPERATION_MISMATCH
PROMPT_NOT_PENDING PROMPT_KIND_MISMATCH PROMPT_DECISION_NOT_ALLOWED SNAPSHOT_EXPIRED SUBSCRIPTION_NOT_FOUND
CONTENT_REF_EXPIRED CHECKPOINT_NOT_FOUND CHECKPOINT_SCOPE_UNAVAILABLE REWIND_PARTIAL STALE_CURSOR
PATH_NOT_FOUND NOT_FILE GIT_UNAVAILABLE GIT_OBJECT_NOT_FOUND QUERY_FAILED CAPABILITY_UNAVAILABLE`)
	sort.Strings(wantCodes)
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Fatalf("Reasonix error code golden drift\n got: %v\nwant: %v", gotCodes, wantCodes)
	}
}

func TestRemoteErrorConditionalFields(t *testing.T) {
	if _, err := NewRemoteError(ErrHostBusy, ErrorOptions{}); err == nil {
		t.Fatal("HOST_BUSY accepted without retryAfterMs")
	}
	negative := int64(-1)
	if _, err := NewRemoteError(ErrHostBusy, ErrorOptions{RetryAfterMs: &negative}); err == nil {
		t.Fatal("HOST_BUSY accepted negative retryAfterMs")
	}
	retry := int64(1)
	if _, err := NewRemoteError(ErrSessionBusy, ErrorOptions{RetryAfterMs: &retry}); err == nil {
		t.Fatal("non-HOST_BUSY accepted retryAfterMs")
	}
	if _, err := NewRemoteError(ErrRewindPartial, ErrorOptions{}); err == nil {
		t.Fatal("REWIND_PARTIAL accepted without explicit impact flags")
	}
	data := MustRemoteError(ErrHostStopped, ErrorOptions{}).Data
	data.SuggestedCommand = "reasonix remote start; rm -rf /"
	if err := data.Validate(); err == nil {
		t.Fatal("mutated uncontrolled command passed validation")
	}
	if _, err := NewRemoteError(ErrVersionMismatch, ErrorOptions{Expected: "/home/user/build", Actual: "safe"}); err == nil {
		t.Fatal("absolute path accepted in controlled mismatch diagnostics")
	}
	workspace, conversation, snapshot := false, false, true
	if _, err := NewRemoteError(ErrRewindPartial, ErrorOptions{WorkspaceMayHaveChanged: &workspace, ConversationMayHaveChanged: &conversation, SnapshotRequired: &snapshot}); err == nil {
		t.Fatal("REWIND_PARTIAL accepted with no possibly changed range")
	}
}
