package agent

import "testing"

func TestRecoveryFilenameParentAndRoot(t *testing.T) {
	t.Parallel()
	parent, ok := RecoveryFilenameParentID("/tmp/session-recovery-aabbccddeeff0011.jsonl")
	if !ok || parent != "session" {
		t.Fatalf("parent = %q ok=%v", parent, ok)
	}
	root, ok := RecoveryFilenameRootID("/tmp/session-recovery-aabbccddeeff0011.jsonl")
	if !ok || root != "session" {
		t.Fatalf("root = %q ok=%v", root, ok)
	}
	if LooksLikeRecoveryFilename("/tmp/session.jsonl") {
		t.Fatal("plain session should not match")
	}
	if !LooksLikeRecoveryFilename("/tmp/session-recovery-AABBCCDDEEFF0011.jsonl") {
		t.Fatal("uppercase hex should match")
	}
}
