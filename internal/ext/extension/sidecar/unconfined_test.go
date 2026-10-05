package sidecar

import (
	"context"
	"testing"
)

func TestStartedSidecarIsRecordedAsUnconfined(t *testing.T) {
	was := unconfinedLaunch.Load()
	unconfinedLaunch.Store(false)
	t.Cleanup(func() { unconfinedLaunch.Store(was) })

	pkg, installed := fakeSidecarPackage(t, "fakeplugin", nil)
	client, err := StartClient(context.Background(), ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()})
	if err != nil {
		t.Fatalf("StartClient: %v", err)
	}
	defer client.Close()
	if !LaunchedUnconfinedProcess() {
		t.Fatal("a sidecar ran outside the sandbox without being recorded")
	}
}
