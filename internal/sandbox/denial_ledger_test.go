package sandbox

import "testing"

func TestDenialLedgerIsExactAndSingleUse(t *testing.T) {
	id := IssueDenial("echo one", "workspace-write")
	if id == "" {
		t.Fatal("IssueDenial returned an empty id")
	}
	if ConsumeDenial(id, "echo two") {
		t.Fatal("denial token authorized a different command")
	}
	if ConsumeDenial(id, "echo one") {
		t.Fatal("failed exact check did not consume denial token")
	}
	id = IssueDenial("echo one", "workspace-write")
	if !ConsumeDenial(id, "echo one") {
		t.Fatal("exact current denial token was rejected")
	}
	if ConsumeDenial(id, "echo one") {
		t.Fatal("denial token was reusable")
	}
}
