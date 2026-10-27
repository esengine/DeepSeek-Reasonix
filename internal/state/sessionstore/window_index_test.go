package sessionstore

import (
	"os"
	"path/filepath"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
	"testing"
)

func TestWindowIndexReadCostDoesNotGrowWithHistory(t *testing.T) {
	for _, count := range []int{100, 10000} {
		t.Run(string(rune('a'+count/10000)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			s := NewSession("sys")
			for range count {
				s.Add(provider.Message{Role: provider.RoleAssistant, Content: "synthetic history"})
			}
			if err := s.Save(path); err != nil {
				t.Fatal(err)
			}
			page, err := ReadStoredMessageWindow(path, -1, 10)
			if err != nil {
				t.Fatal(err)
			}
			if page.IndexBytesRead != windowHeaderBytes+10*windowEntryBytes || page.DecodedMessages != 10 {
				t.Fatalf("history=%d index_bytes=%d decoded=%d", count, page.IndexBytesRead, page.DecodedMessages)
			}
			t.Logf("history_messages=%d page_messages=10 index_bytes=%d decoded_messages=%d", count, page.IndexBytesRead, page.DecodedMessages)
		})
	}
}

func TestWindowIndexRepairsDamagedPageMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "preserved"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(store.SessionWindowIndex(path), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0xff}, windowHeaderBytes+windowEntryBytes); err != nil {
		t.Fatal(err)
	}
	f.Close()
	page, err := ReadStoredMessageWindow(path, -1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Content != "preserved" {
		t.Fatalf("page=%+v", page)
	}
}

func BenchmarkStoredWindowIndex(b *testing.B) {
	path := filepath.Join(b.TempDir(), "session.jsonl")
	s := NewSession("sys")
	for range 100000 {
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "synthetic history for bounded paging benchmark"})
	}
	if err := s.Save(path); err != nil {
		b.Fatal(err)
	}
	b.Run("fixed-page", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := ReadStoredMessageWindow(path, -1, 20); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("legacy-full-index", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := currentPageIndex(path); err != nil {
				b.Fatal(err)
			}
		}
	})
}
