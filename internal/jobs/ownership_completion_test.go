package jobs

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRuntimeStateAdoptionAndCompletionShareOwnership(t *testing.T) {
	for _, phase := range []string{"before-completion", "during-notice", "after-done"} {
		t.Run(phase, func(t *testing.T) {
			sink := &blockingFinishedSink{entered: make(chan struct{}), released: make(chan struct{})}
			m := NewManager(sink)
			defer m.Close()
			runRelease := make(chan struct{})
			releaseRun := sync.OnceFunc(func() { close(runRelease) })
			releaseNotice := sync.OnceFunc(func() { close(sink.released) })
			defer releaseRun()
			defer releaseNotice()
			job := m.StartForSession("", "bash", "adoption", func(context.Context, io.Writer) (string, error) {
				<-runRelease
				return "complete", nil
			})
			path := filepath.Join(t.TempDir(), "adopted.jsonl")
			if phase == "before-completion" {
				m.SetActiveSessionPath("adopted", path)
			}
			releaseRun()
			<-sink.entered
			select {
			case <-job.done:
				t.Fatal("done closed before completion notice finished")
			default:
			}
			if phase == "during-notice" {
				m.SetActiveSessionPath("adopted", path)
			}
			releaseNotice()
			<-job.done
			if phase == "after-done" {
				m.SetActiveSessionPath("adopted", path)
			}
			if got := m.DrainCompletedNoteForSession("other"); got != "" {
				t.Fatalf("completion leaked: %s", got)
			}
			if got := m.DrainCompletedNoteForSession("adopted"); strings.Count(got, job.ID) != 1 {
				t.Fatalf("completion not owned exactly once: %q", got)
			}
			if got := m.DrainCompletedNoteForSession("adopted"); got != "" {
				t.Fatalf("completion replayed: %q", got)
			}
			job.mu.Lock()
			metaPath := job.artifactMetaPath
			job.mu.Unlock()
			if metaPath != "" {
				data, err := os.ReadFile(metaPath)
				if err != nil {
					t.Fatal(err)
				}
				var meta artifactMeta
				if err := json.Unmarshal(data, &meta); err != nil {
					t.Fatal(err)
				}
				if meta.SessionID != "adopted" {
					t.Fatalf("artifact kept old owner: %q", meta.SessionID)
				}
			}
		})
	}
}
