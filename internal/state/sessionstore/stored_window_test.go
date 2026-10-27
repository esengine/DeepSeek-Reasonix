package sessionstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
	"reflect"
	"testing"
)

func TestStoredWindowReadsOnlyPageAndItsCrossPageCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "child.jsonl")
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "read_file", Arguments: `{"path":"x"}`}}})
	for range 100 {
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "old history"})
	}
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call", Content: "result"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	page, err := ReadStoredMessageWindow(path, -1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || len(page.ToolContext) != 1 || page.ToolContext[0].Call.Name != "read_file" {
		t.Fatalf("page=%+v", page)
	}
	if page.DecodedMessages != 2 {
		t.Fatalf("decoded %d messages for a one-message page", page.DecodedMessages)
	}
	if page.IndexBytesRead != windowHeaderBytes+2*windowEntryBytes {
		t.Fatalf("index read=%d, want two entries independent of history length", page.IndexBytesRead)
	}
	if !reflect.DeepEqual(page.Messages, s.ReadMessageWindow(-1, 1).Messages) {
		t.Fatal("stored page changed content")
	}
}

func TestStoredWindowRepairsOldMissingAndCorruptDerivedIndexes(t *testing.T) {
	for _, damage := range []string{"old", "missing", "corrupt", "stale"} {
		t.Run(damage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "child.jsonl")
			s := NewSession("system")
			s.Add(provider.Message{Role: provider.RoleAssistant, Content: "original"})
			if err := s.Save(path); err != nil {
				t.Fatal(err)
			}
			before, err := ReadStoredMessageWindow(path, -1, 1)
			if err != nil {
				t.Fatal(err)
			}
			indexPath := store.SessionDisplayIndex(path)
			if err := os.Remove(store.SessionWindowIndex(path)); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "old":
				var idx SessionDisplayIndex
				if err := json.Unmarshal(raw, &idx); err != nil {
					t.Fatal(err)
				}
				idx.PageMetadata = 0
				if err := WriteSessionDisplayIndex(indexPath, &idx); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(indexPath); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(indexPath, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "stale":
				s.Rewrite([]provider.Message{{Role: provider.RoleAssistant, Content: "replaced"}}, "test")
				if err := s.Save(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(indexPath, raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(store.SessionWindowIndex(path)); err != nil {
					t.Fatal(err)
				}
			}
			page, err := ReadStoredMessageWindow(path, -1, 1)
			if err != nil {
				t.Fatal(err)
			}
			want := "original"
			if damage == "stale" {
				want = "replaced"
				if page.Identity == before.Identity {
					t.Fatal("rewrite kept snapshot identity")
				}
			}
			if page.Messages[0].Content != want {
				t.Fatalf("page=%+v", page)
			}
			if _, err := currentPageIndex(path); err != nil {
				t.Fatalf("repair not reusable: %v", err)
			}
		})
	}
}

func TestStoredWindowPreservesDuplicateCallIDsAndWorkDurations(t *testing.T) {
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "reused", Name: "old"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "reused", Content: "old result"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done", WorkDurationMs: 123})
	s.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "reused", Name: "new"}, {ID: "other", Name: "other"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "other", Content: "other result"})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "reused", Content: "new result"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done", WorkDurationMs: 456})
	path := filepath.Join(t.TempDir(), "child.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	for start := range len(s.Messages) {
		want := s.ReadMessageWindow(start, 3)
		got, err := ReadStoredMessageWindow(path, start, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want.Messages, got.Messages) || !reflect.DeepEqual(want.WorkMs, got.WorkMs) || !reflect.DeepEqual(want.ToolContext, got.ToolContext) {
			t.Fatalf("page %d differs: want %+v got %+v", start, want, got)
		}
	}
}
