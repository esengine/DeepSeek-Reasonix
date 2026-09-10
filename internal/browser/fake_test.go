package browser

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/tool"
)

// fakeExecutor records every call and answers from its fixed fields; err,
// when set, is returned by every method.
type fakeExecutor struct {
	err        error
	tabs       []Tab
	snapshot   Snapshot
	screenshot Screenshot
	act        ActResult
	downloads  []Download

	calls  []string
	opens  []OpenRequest
	navs   []NavigateRequest
	snaps  []SnapshotRequest
	shots  []ScreenshotRequest
	acts   []ActRequest
	dls    []DownloadsRequest
	closed []string
	closes []CloseRequest
}

func (f *fakeExecutor) Tabs(context.Context) ([]Tab, error) {
	f.calls = append(f.calls, "tabs")
	return f.tabs, f.err
}

func (f *fakeExecutor) Open(_ context.Context, req OpenRequest) (Tab, error) {
	f.calls = append(f.calls, "open")
	f.opens = append(f.opens, req)
	if f.err != nil {
		return Tab{}, f.err
	}
	return Tab{ID: "t-new", URL: req.URL, Temporary: req.Temporary}, nil
}

func (f *fakeExecutor) Navigate(_ context.Context, req NavigateRequest) (Tab, error) {
	f.calls = append(f.calls, "navigate")
	f.navs = append(f.navs, req)
	if f.err != nil {
		return Tab{}, f.err
	}
	return Tab{ID: req.TabID, URL: req.URL}, nil
}

func (f *fakeExecutor) Snapshot(_ context.Context, req SnapshotRequest) (Snapshot, error) {
	f.calls = append(f.calls, "snapshot")
	f.snaps = append(f.snaps, req)
	return f.snapshot, f.err
}

func (f *fakeExecutor) Screenshot(_ context.Context, req ScreenshotRequest) (Screenshot, error) {
	f.calls = append(f.calls, "screenshot")
	f.shots = append(f.shots, req)
	return f.screenshot, f.err
}

func (f *fakeExecutor) Act(_ context.Context, req ActRequest) (ActResult, error) {
	f.calls = append(f.calls, "act")
	f.acts = append(f.acts, req)
	return f.act, f.err
}

func (f *fakeExecutor) Downloads(_ context.Context, req DownloadsRequest) ([]Download, error) {
	f.calls = append(f.calls, "downloads")
	f.dls = append(f.dls, req)
	return f.downloads, f.err
}

func (f *fakeExecutor) Close(_ context.Context, req CloseRequest) error {
	f.calls = append(f.calls, "close")
	f.closed = append(f.closed, req.TabID)
	f.closes = append(f.closes, req)
	return f.err
}

// gatedExecutor adds Availability to the fake.
type gatedExecutor struct {
	*fakeExecutor
	available bool
}

func (g gatedExecutor) Available(context.Context) bool { return g.available }

func toolByName(t *testing.T, exec Executor, name string) tool.Tool {
	t.Helper()
	for _, tl := range Tools(exec) {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

func run(t *testing.T, exec Executor, name, args string) (string, error) {
	t.Helper()
	return toolByName(t, exec, name).Execute(context.Background(), json.RawMessage(args))
}
