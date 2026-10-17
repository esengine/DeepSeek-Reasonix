package serve

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestAttachmentStreamUserLimitRefusesExtraByteAndCleansPartial(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[attachments]\nmax_stream_bytes = 8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A project cannot disable the user's disk guard.
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[attachments]\nmax_stream_bytes = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := attachmentServer(t, root)
	// LimitReader has no advertised Content-Length; the server must count bytes.
	resp, err := http.Post(srv.URL+"/attachments/upload?name=large.apk", "application/octet-stream", io.LimitReader(repeatingByte{}, 9))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var why Reason
	if err := json.NewDecoder(resp.Body).Decode(&why); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge || why.Code != "attachment.too_large" {
		t.Fatalf("status=%d reason=%+v", resp.StatusCode, why)
	}
	if why.Params["limit_bytes"] != float64(8) || why.Params["limit_source"] != "user" {
		t.Fatalf("limit facts = %+v", why.Params)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".gitignore" {
			t.Fatalf("partial attachment retained: %s", entry.Name())
		}
	}
	resp, err = http.Post(srv.URL+"/attachments/upload?name=exact.apk", "application/octet-stream", bytes.NewReader(make([]byte, 8)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exact limit refused: %d", resp.StatusCode)
	}
}

func TestAttachmentStreamExplicitUnlimitedAndMalformedUserConfig(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("[attachments]\nmax_stream_bytes=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := attachmentServer(t, root)
	resp, err := http.Post(srv.URL+"/attachments/upload?name=unlimited.apk", "application/octet-stream", bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("explicit unlimited refused: %d", resp.StatusCode)
	}
	before, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[attachments\nmax_stream_bytes=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp, err = http.Post(srv.URL+"/attachments/upload?name=bad.apk", "application/octet-stream", bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var why Reason
	if err := json.NewDecoder(resp.Body).Decode(&why); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError || why.Code != "attachment.config_unavailable" {
		t.Fatalf("malformed config ignored: %d %+v", resp.StatusCode, why)
	}
	after, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil || len(before) != len(after) {
		t.Fatalf("malformed config created an attachment: %v", err)
	}
}
