package jobs

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestToolResultPreservesUnknownFieldsAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	raw := json.RawMessage(`{"content":[{"type":"resource_link","uri":"https://example.test/video.mp4","mimeType":"video/mp4","future":{"x":1}}],"structuredContent":{"job_id":"j","seed":9007199254740993},"future":{"token":"opaque"}}`)
	m := NewManager(nil)
	m.SetActiveSessionPath("session", sessionPath)
	ref, err := m.SaveToolResultForSession("session", "../../call", raw)
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	m = NewManager(nil)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)
	f, err := m.OpenToolResultForSession("session", ref)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var saved toolResultArtifact
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	var want, got map[string]json.RawMessage
	_ = json.Unmarshal(raw, &want)
	_ = json.Unmarshal(saved.Result, &got)
	if saved.Version != 1 || !reflect.DeepEqual(want, got) {
		t.Fatalf("saved result lost fields: %s", b)
	}
	name, err := resultFilename(ref)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ArtifactDir(sessionPath), name)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private result mode: %v, %v", info, err)
	}
	views, err := ListArtifactViews(sessionPath)
	if err != nil || len(views) != 0 {
		t.Fatalf("result misread as job: %v, %v", views, err)
	}
}

func TestToolResultRejectsInvalidJSONAndSession(t *testing.T) {
	m := NewManager(nil)
	defer m.Close()
	if _, err := m.SaveToolResultForSession("../escape", "call", json.RawMessage(`{}`)); err == nil {
		t.Fatal("accepted invalid session")
	}
	if _, err := m.SaveToolResultForSession("session", "call", json.RawMessage(`{`)); err == nil {
		t.Fatal("accepted invalid result")
	}
}

func TestToolResultReusedCallIDDoesNotOverwriteEarlierResult(t *testing.T) {
	m := NewManager(nil)
	defer m.Close()
	a, err := m.SaveToolResultForSession("session", "call-1", json.RawMessage(`{"structuredContent":{"version":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.SaveToolResultForSession("session", "call-1", json.RawMessage(`{"structuredContent":{"version":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("reused call identity overwrote canonical result")
	}
	f, err := m.OpenToolResultForSession("session", a)
	if err != nil {
		t.Fatal(err)
	}
	first, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil || !bytes.Contains(first, []byte(`"version":1`)) {
		t.Fatalf("earlier result changed: %s %v", first, err)
	}
}

func TestToolResultReferenceSurvivesBindingAndRejectsOtherSessions(t *testing.T) {
	m := NewManager(nil)
	ref, err := m.SaveToolResultForSession("", "call", json.RawMessage(`{"structuredContent":{"video":"original"}}`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "first.jsonl")
	m.SetActiveSessionPath("first", path)
	if f, err := m.OpenToolResultForSession("first", ref); err != nil {
		t.Fatal(err)
	} else {
		_ = f.Close()
	}
	second := filepath.Join(t.TempDir(), "second.jsonl")
	m.SetActiveSessionPath("second", second)
	if f, err := m.OpenToolResultForSession("second", ref); err == nil {
		_ = f.Close()
		t.Fatal("result moved into unrelated session")
	}
	m.Close()
	m2 := NewManager(nil)
	defer m2.Close()
	m2.SetActiveSessionPath("first", path)
	if f, err := m2.OpenToolResultForSession("first", ref); err != nil {
		t.Fatal(err)
	} else {
		_ = f.Close()
	}
	for _, bad := range []string{"tool-result:../secret", "tool-result:" + strings.Repeat("z", 64), "file:///etc/passwd"} {
		if f, err := m2.OpenToolResultForSession("first", bad); err == nil {
			_ = f.Close()
			t.Fatalf("accepted %s", bad)
		}
	}
}
