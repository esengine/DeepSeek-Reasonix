package main

import "testing"

func TestPickRemoteIdentityFileWithoutDesktopContextIsCancelled(t *testing.T) {
	app := &App{}
	path, err := app.PickRemoteIdentityFile()
	if err != nil {
		t.Fatalf("PickRemoteIdentityFile: %v", err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty cancellation", path)
	}
}
