package account

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRegisterRemoteDeviceKeepsTheCredentialAtTheClientBoundary(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/devices" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer account-token" {
			t.Errorf("authorization = %q", got)
		}
		var body RemoteDeviceRegistration
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.PublicKey != "pkey" || body.Platform != "macos" {
			t.Errorf("registration = %+v", body)
		}
		writeJSON(t, w, 201, map[string]any{
			"device":           map[string]any{"id": "device-id", "name": "Home Mac", "platform": "macos", "publicKey": "pkey", "capabilities": []string{"tasks"}},
			"deviceCredential": "device-secret",
		})
	})

	registered, err := c.RegisterRemoteDevice(context.Background(), "account-token", RemoteDeviceRegistration{
		Name: "Home Mac", Platform: "macos", PublicKey: "pkey", Capabilities: []RemoteCapability{RemoteTasks},
	})
	if err != nil {
		t.Fatal(err)
	}
	if registered.Device.ID != "device-id" || registered.DeviceCredential != "device-secret" {
		t.Errorf("registered = %+v", registered)
	}
}

func TestIssueRemoteAttachmentReadsSeparateTickets(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TargetDeviceID  string `json:"targetDeviceId"`
			CiphertextBytes int64  `json:"ciphertextBytes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.TargetDeviceID != "target" || body.CiphertextBytes != 4096 {
			t.Errorf("grant request = %+v", body)
		}
		writeJSON(t, w, 201, map[string]any{"attachment": map[string]any{
			"objectId": "object", "uploadTicket": "upload", "downloadTicket": "download",
			"maxBytes": 4096, "expiresAt": "2026-09-27T00:15:00.000Z",
		}})
	})

	grant, err := c.IssueRemoteAttachment(context.Background(), "account-token", "target", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if grant.UploadTicket == grant.DownloadTicket || grant.MaxBytes != 4096 {
		t.Errorf("grant = %+v", grant)
	}
}
