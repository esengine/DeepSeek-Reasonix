package qq

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestQQRawGatewayEventRemovedOnlyForMatchingAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	payload := gatewayPayload{T: "C2C_MESSAGE_CREATE", S: 7, D: json.RawMessage(`{"id":"event-1","content":"hello"}`)}
	if err := saveQQGatewayRawEvent("app-a", false, payload); err != nil {
		t.Fatal(err)
	}
	if err := saveQQGatewayRawEvent("app-b", false, payload); err != nil {
		t.Fatal(err)
	}
	if err := removeQQGatewayRawEvent("app-a", false, "event-1"); err != nil {
		t.Fatal(err)
	}
	if got, err := loadQQGatewayRawEvents("app-a", false); err != nil || len(got) != 0 {
		t.Fatalf("app-a replay = %v, %v", got, err)
	}
	if got, err := loadQQGatewayRawEvents("app-b", false); err != nil || len(got) != 1 {
		t.Fatalf("app-b replay = %v, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "qq-gateway-events"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("raw event files = %v, %v", entries, err)
	}
}
