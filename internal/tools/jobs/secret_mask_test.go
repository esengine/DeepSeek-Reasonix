package jobs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/secrets/secretstest"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

// A background job's log sits in the sessions folder, where other commands can
// read it: what it keeps is masked as it is written, a value split across two
// writes included, and so is the result a job returns.
func TestABackgroundJobKeepsItsOutputMasked(t *testing.T) {
	const value = "sk-job-log-0123456789"
	secretstest.Hold("JOB_KEY", value)
	sessionPath := filepath.Join(testenv.TempDir(t), "session.jsonl")
	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", sessionPath)
	j := m.StartForSession("session", "bash", "server", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, "config key=sk-job-l")
		_, _ = io.WriteString(out, "og-0123456789 loaded\n")
		return "finished with " + value, nil
	})
	<-j.done
	j.mu.Lock()
	path := j.artifact.path
	j.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-job") || !strings.Contains(string(data), "key=[secret:JOB_KEY] loaded") || !strings.Contains(string(data), "finished with [secret:JOB_KEY]") {
		t.Fatalf("log on disk = %q", data)
	}
}

// A job that fails with no result reports its error in the result's place;
// that text is masked too.
func TestAFailedJobsErrorIsMasked(t *testing.T) {
	secretstest.Hold("JOB_ERR_KEY", "sk-job-err-0123456789")
	m := NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", filepath.Join(testenv.TempDir(t), "session.jsonl"))
	j := m.StartForSession("session", "bash", "fails", func(context.Context, io.Writer) (string, error) {
		return "", errors.New("auth failed with sk-job-err-0123456789")
	})
	<-j.done
	j.mu.Lock()
	path := j.artifact.path
	j.mu.Unlock()
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "sk-job-err") {
		t.Fatalf("log = %q", data)
	}
}
