package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/transcript"
)

func TestTranscriptNewSessionBeforeFirstSave(t *testing.T) {
	dir := t.TempDir()
	bc := NewBroadcaster()
	previous := agent.NewSession("system")
	previous.Add(provider.Message{Role: provider.RoleUser, Content: "previous conversation"})
	previous.Add(provider.Message{Role: provider.RoleAssistant, Content: "previous reply"})
	previousPath := filepath.Join(dir, "previous.jsonl")
	if err := previous.Save(previousPath); err != nil {
		t.Fatal(err)
	}
	ctrl := control.New(control.Options{Executor: agent.New(nil, nil, previous, agent.Options{}, bc), SessionDir: dir, SessionPath: previousPath, Sink: bc})
	defer ctrl.Close()
	server := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer server.Close()
	response, err := http.Post(server.URL+"/new", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	path := response.Header.Get(sessionPathHeader)
	if response.StatusCode != http.StatusNoContent || path == "" {
		t.Fatalf("new: status=%d path=%q", response.StatusCode, path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("fresh transcript should not exist yet: %v", err)
	}
	response, err = http.Get(server.URL + "/transcript/snapshot?session=" + url.QueryEscape(path))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fresh snapshot: status=%d", response.StatusCode)
	}
	var snap transcript.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Records) != 0 || snap.Identity.SessionID != agent.BranchID(path) {
		t.Fatalf("fresh snapshot: %+v", snap)
	}
}

func TestTranscriptHTTPBindsSessionAndImmutableContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("body", 20000)})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	executor := agent.New(nil, nil, session, agent.Options{}, bc)
	ctrl := control.New(control.Options{Executor: executor, SessionDir: dir, SessionPath: path, Sink: bc})
	defer ctrl.Close()
	server := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/transcript/snapshot?session=" + url.QueryEscape(path))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("snapshot status=%d cache=%q", response.StatusCode, response.Header.Get("Cache-Control"))
	}
	var snapshot transcript.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ProtocolVersion != 1 || len(snapshot.Records) != 2 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	ref := snapshot.Records[1].Refs[0]
	encoded, _ := json.Marshal(transcript.ContentRequest{ContentRef: ref})
	contentResponse, err := http.Get(server.URL + "/transcript/content?session=" + url.QueryEscape(path) + "&request=" + url.QueryEscape(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	defer contentResponse.Body.Close()
	var content transcript.ContentChunk
	if err := json.NewDecoder(contentResponse.Body).Decode(&content); err != nil || len(content.Data) != 64<<10 {
		t.Fatalf("content bytes=%d err=%v", len(content.Data), err)
	}
	wrong, err := http.Get(server.URL + "/transcript/snapshot?session=" + url.QueryEscape(filepath.Join(dir, "different.jsonl")))
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusConflict {
		t.Fatalf("wrong-session status=%d", wrong.StatusCode)
	}
	malformed, err := http.Get(server.URL + "/transcript/page?request=%7B")
	if err != nil {
		t.Fatal(err)
	}
	malformed.Body.Close()
	if malformed.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed status=%d", malformed.StatusCode)
	}
}
