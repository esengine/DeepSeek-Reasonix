package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

func titlePost(t *testing.T, h *Hub, route, path, title string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"path": path, "title": title})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/tree/sessions/"+route, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.AuthToken())
	res := httptest.NewRecorder()
	h.Handler().ServeHTTP(res, req)
	return res
}

func titleSession(t *testing.T) (*Hub, *Runtime, string, string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	rt := hubRuntime(t, h, root)
	path := filepath.Join(SessionDirFor(root), "20261001-120000.jsonl")
	s := sessionstore.NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first topic"})
	latest := strings.Repeat("recent discussion ", 25) + "last user intent"
	s.Add(provider.Message{Role: provider.RoleUser, Content: "compiled context", RawContent: latest})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "assistant answer"})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "host notification", HostAuthored: true})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := sessionstore.UpdateBranchMeta(path, false, func(meta *sessionstore.BranchMeta) error {
		meta.CustomTitle = "old custom name"
		meta.TopicTitle = "original topic"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rt.Server.titles.put(filepath.Base(path), "old automatic name", "first topic", 1)
	return h, rt, path, latest
}

func TestAutoNameUsesUserMessagesSuffixAndWaitsForSave(t *testing.T) {
	h, rt, path, latest := titleSession(t)
	source := []rune("first topic\n" + latest)
	expected := string(source[len(source)-300:])
	before, _, err := sessionstore.LoadBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(filepath.Dir(path), ".session-titles.json")
	beforeCache, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	prov := &recordingTitleProvider{}
	rt.Server.titleProv = prov
	h.workspaceSessions(rt.Root, nil)
	res := titlePost(t, h, "auto-name", path, "")
	if res.Code != http.StatusOK {
		t.Fatalf("auto-name: %d %s", res.Code, res.Body.String())
	}
	req := prov.at(0)
	if len(req.Messages) != 2 || req.Messages[1].Content != expected {
		t.Fatalf("model did not receive the last 300 characters of joined user messages: %+v", req.Messages)
	}
	var generated struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &generated); err != nil || generated.Title != expected {
		t.Fatalf("generated draft = %+v, err=%v", generated, err)
	}
	afterDraft, _, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !reflect.DeepEqual(afterDraft, before) {
		t.Fatalf("generation changed metadata: got=%+v want=%+v err=%v", afterDraft, before, err)
	}
	afterCache, err := os.ReadFile(cachePath)
	if err != nil || !bytes.Equal(beforeCache, afterCache) {
		t.Fatalf("generation changed the title cache: %v", err)
	}
	res = titlePost(t, h, "rename", path, generated.Title)
	if res.Code != http.StatusNoContent {
		t.Fatalf("save generated title: %d %s", res.Code, res.Body.String())
	}
	wantMeta := before
	wantMeta.CustomTitle = expected
	after, _, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !reflect.DeepEqual(after, wantMeta) {
		t.Fatalf("automatic naming changed metadata beyond the title: got=%+v want=%+v err=%v", after, wantMeta, err)
	}
	rt.Server.titles.put(filepath.Base(path), "late background result", "first topic", 2)
	rows := h.workspaceSessions(rt.Root, nil)
	if len(rows) != 1 || rows[0].Title != expected {
		t.Fatalf("tree title: %+v", rows)
	}
	if got := rt.Server.sessionTitle(filepath.Base(path), "changed first message", 3, "old custom name"); got != expected {
		t.Fatalf("pane title = %q", got)
	}
	entries := map[string]titleEntry{}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), ".session-titles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if !entries[filepath.Base(path)].Explicit || entries[filepath.Base(path)].Title != expected {
		t.Fatalf("persisted entry = %+v", entries)
	}
	fresh := NewHub(HubOptions{})
	if rows = fresh.workspaceSessions(rt.Root, nil); len(rows) != 1 || rows[0].Title != expected {
		t.Fatalf("reloaded title = %+v", rows)
	}
	res = titlePost(t, h, "rename", path, "manual replacement")
	if res.Code != http.StatusNoContent {
		t.Fatalf("rename: %d %s", res.Code, res.Body.String())
	}
	if rows = fresh.workspaceSessions(rt.Root, nil); rows[0].Title != "manual replacement" {
		t.Fatalf("manual title = %+v", rows)
	}
	wantMeta.CustomTitle = "manual replacement"
	after, _, err = sessionstore.LoadBranchMeta(path)
	if err != nil || !reflect.DeepEqual(after, wantMeta) {
		t.Fatalf("manual naming changed metadata beyond the title: got=%+v want=%+v err=%v", after, wantMeta, err)
	}
}

type failingRenameProvider struct{}

func (failingRenameProvider) Name() string { return "failed-title" }
func (failingRenameProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("model unavailable")
}

func TestAutoNameFailurePreservesTitleAndCache(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	rt.Server.titleProv = failingRenameProvider{}
	cachePath := filepath.Join(filepath.Dir(path), ".session-titles.json")
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	res := titlePost(t, h, "auto-name", path, "")
	after, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusBadGateway || !bytes.Equal(before, after) {
		t.Fatalf("failed rename changed cache: %d %s", res.Code, res.Body.String())
	}
	rows := h.workspaceSessions(rt.Root, nil)
	if len(rows) != 1 || rows[0].Title != "old custom name" {
		t.Fatalf("failed rename changed title: %+v", rows)
	}
}

func TestAutoNameRefusesMissingUserMessageAndOutsidePath(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	prov := &recordingTitleProvider{}
	rt.Server.titleProv = prov
	path = filepath.Join(filepath.Dir(path), "20261001-120001.jsonl")
	empty := sessionstore.NewSession("system only")
	if err := empty.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	res := titlePost(t, h, "auto-name", path, "")
	if res.Code != http.StatusBadRequest || prov.count() != 0 {
		t.Fatalf("empty session = %d %s", res.Code, res.Body.String())
	}
	outside := filepath.Join(testenv.TempDir(t), "20261001-120000.jsonl")
	res = titlePost(t, h, "auto-name", outside, "")
	if res.Code != http.StatusForbidden || prov.count() != 0 {
		t.Fatalf("outside session = %d %s", res.Code, res.Body.String())
	}
}

type stalledTitleProvider struct{}

func (stalledTitleProvider) Name() string { return "stalled-title" }
func (stalledTitleProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return make(chan provider.Chunk), nil
}

func TestTitleRequestStopsAtDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	s := &Server{titleProv: stalledTitleProvider{}}
	if got := s.requestTitle(ctx, "latest message"); got != "" {
		t.Fatalf("timed out request produced %q", got)
	}
}

func TestWriteSessionTitleRollsBackMetadataOnCacheFailure(t *testing.T) {
	for _, existing := range []bool{true, false} {
		name := "existing metadata"
		if !existing {
			name = "missing metadata"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(testenv.TempDir(t), "20261002-140000.jsonl")
			metaPath := sessionstore.BranchMetaPath(path)
			original := []byte(`{"id":"original","custom_title":"old title","topic_title":"original topic","revision":7}`)
			var originalMode os.FileMode
			if existing {
				if err := os.WriteFile(metaPath, original, 0o600); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(metaPath)
				if err != nil {
					t.Fatal(err)
				}
				originalMode = info.Mode().Perm()
			}
			cacheErr := errors.New("cache write failed")
			err := writeSessionTitle(path, "new title", func() error {
				meta, _, err := sessionstore.LoadBranchMeta(path)
				if err != nil || meta.CustomTitle != "new title" {
					t.Fatalf("metadata was not written before cache publish: %+v err=%v", meta, err)
				}
				return cacheErr
			})
			if !errors.Is(err, cacheErr) {
				t.Fatalf("write error = %v", err)
			}
			after, err := os.ReadFile(metaPath)
			if existing {
				info, statErr := os.Stat(metaPath)
				if err != nil || !bytes.Equal(after, original) || statErr != nil || info.Mode().Perm() != originalMode {
					t.Fatalf("original metadata was not restored: %s err=%v", after, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed write left new metadata behind: %s err=%v", after, err)
			}
		})
	}
}

func TestCorruptTitleCacheRecoversForRename(t *testing.T) {
	for _, route := range []string{"rename", "auto-name"} {
		t.Run(route, func(t *testing.T) {
			h, rt, path, latest := titleSession(t)
			rt.Server.titleProv = &recordingTitleProvider{}
			cachePath := filepath.Join(filepath.Dir(path), ".session-titles.json")
			damaged := "broken cache"
			if route == "auto-name" {
				damaged = `{"stale.jsonl":{"title":"stale title","explicit":true},"broken.jsonl":{"title":123}}`
			}
			if err := os.WriteFile(cachePath, []byte(damaged), 0o600); err != nil {
				t.Fatal(err)
			}
			wantTitle := "recovered manual title"
			wantStatus := http.StatusNoContent
			if route == "auto-name" {
				source := []rune("first topic\n" + latest)
				wantTitle = string(source[len(source)-300:])
				wantStatus = http.StatusOK
			}
			res := titlePost(t, h, route, path, "recovered manual title")
			if res.Code != wantStatus {
				t.Fatalf("rename after corruption = %d %s", res.Code, res.Body.String())
			}
			if route == "auto-name" {
				data, err := os.ReadFile(cachePath)
				if err != nil || string(data) != damaged {
					t.Fatalf("generation modified corrupt cache: %v", err)
				}
				res = titlePost(t, h, "rename", path, wantTitle)
				if res.Code != http.StatusNoContent {
					t.Fatalf("save generated title after corruption: %d %s", res.Code, res.Body.String())
				}
			}
			data, err := os.ReadFile(cachePath)
			if err != nil {
				t.Fatal(err)
			}
			var entries map[string]titleEntry
			if err := json.Unmarshal(data, &entries); err != nil {
				t.Fatalf("rebuilt cache is invalid: %v", err)
			}
			entry := entries[filepath.Base(path)]
			if len(entries) != 1 || entry.Title != wantTitle || !entry.Explicit {
				t.Fatalf("rebuilt cache = %+v, want only the new title %q", entries, wantTitle)
			}
			rows := h.workspaceSessions(rt.Root, nil)
			if len(rows) != 1 || rows[0].Title != wantTitle {
				t.Fatalf("visible title after recovery = %+v", rows)
			}
		})
	}
}

func TestResetTitleRestoresCachedAutomaticTitle(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	prov := &recordingTitleProvider{}
	rt.Server.titleProv = prov
	before, _, err := sessionstore.LoadBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	res := titlePost(t, h, "rename", path, "   ")
	if res.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", res.Code, res.Body.String())
	}
	meta, _, err := sessionstore.LoadBranchMeta(path)
	before.CustomTitle = ""
	if err != nil || !reflect.DeepEqual(meta, before) {
		t.Fatalf("reset metadata = %+v, want %+v, err=%v", meta, before, err)
	}
	rows := h.workspaceSessions(rt.Root, nil)
	if len(rows) != 1 || rows[0].Title != "old automatic name" || prov.count() != 0 {
		t.Fatalf("reset did not reuse automatic title: rows=%+v calls=%d", rows, prov.count())
	}
}

func TestResetTitleRemovesFixedEntryAndResumesGeneration(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	prov := &recordingTitleProvider{}
	rt.Server.titleProv = prov
	cache := h.titleCacheFor(filepath.Dir(path))
	name := filepath.Base(path)
	if err := cache.putExplicit(name, "fixed title"); err != nil {
		t.Fatal(err)
	}
	res := titlePost(t, h, "rename", path, "")
	if res.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", res.Code, res.Body.String())
	}
	got := waitTitle(t, rt.Server, name, "first topic", sessionstore.SessionContentModTime(path).UnixNano())
	if got != "first topic" {
		t.Fatalf("automatic title = %q", got)
	}
	entry := cache.snapshot()[name]
	meta, _, err := sessionstore.LoadBranchMeta(path)
	if entry.Explicit || meta.CustomTitle != "" || err != nil {
		t.Fatalf("fixed title survived reset: entry=%+v meta=%+v err=%v", entry, meta, err)
	}
	fresh := NewHub(HubOptions{})
	rows := fresh.workspaceSessions(rt.Root, nil)
	if len(rows) != 1 || rows[0].Title != "first topic" {
		t.Fatalf("reopened automatic title = %+v", rows)
	}
}

func TestResetTitleStillSucceedsWhenGenerationFails(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	rt.Server.titleProv = failingRenameProvider{}
	cache := h.titleCacheFor(filepath.Dir(path))
	if err := cache.putExplicit(filepath.Base(path), "fixed title"); err != nil {
		t.Fatal(err)
	}
	res := titlePost(t, h, "rename", path, "")
	meta, _, err := sessionstore.LoadBranchMeta(path)
	rows := h.workspaceSessions(rt.Root, nil)
	if res.Code != http.StatusNoContent || err != nil || meta.CustomTitle != "" || len(rows) != 1 || rows[0].Title != "first topic" {
		t.Fatalf("reset failed instead of falling back to preview: status=%d meta=%+v rows=%+v err=%v", res.Code, meta, rows, err)
	}
}

func TestResetTitleFailureKeepsMetadataAndCache(t *testing.T) {
	h, _, path, _ := titleSession(t)
	cache := h.titleCacheFor(filepath.Dir(path))
	if err := cache.putExplicit(filepath.Base(path), "fixed title"); err != nil {
		t.Fatal(err)
	}
	metaBefore, err := os.ReadFile(sessionstore.BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(filepath.Dir(path), ".session-titles.json")
	cacheBefore, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	backup := cachePath + ".saved"
	if err := os.Rename(cachePath, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cachePath, 0o700); err != nil {
		t.Fatal(err)
	}
	res := titlePost(t, h, "rename", path, "")
	metaAfter, err := os.ReadFile(sessionstore.BranchMetaPath(path))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusInternalServerError || !bytes.Equal(metaBefore, metaAfter) || !bytes.Equal(cacheBefore, saved) {
		t.Fatalf("failed reset changed the fixed title: status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestResetTitleRequiresAnExplicitEmptyString(t *testing.T) {
	h, _, path, _ := titleSession(t)
	cache := h.titleCacheFor(filepath.Dir(path))
	name := filepath.Base(path)
	if err := cache.putExplicit(name, "fixed title"); err != nil {
		t.Fatal(err)
	}
	for _, body := range []map[string]any{{"path": path}, {"path": path, "title": nil}} {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/tree/sessions/rename", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+h.AuthToken())
		res := httptest.NewRecorder()
		h.Handler().ServeHTTP(res, req)
		meta, _, err := sessionstore.LoadBranchMeta(path)
		if res.Code != http.StatusBadRequest || err != nil || meta.CustomTitle != "fixed title" || !cache.snapshot()[name].Explicit {
			t.Fatalf("missing/null title reset the name: status=%d meta=%+v err=%v", res.Code, meta, err)
		}
	}
}
