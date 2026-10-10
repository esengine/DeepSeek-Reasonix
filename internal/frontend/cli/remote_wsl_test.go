package cli

import (
	"runtime"
	"testing"
)

func TestRemoteWSLRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, args := range [][]string{nil, {"enable"}, {"list", "x"}, {"test"}, {"test", "a", "b"}} {
		if got := remoteWSLCLI(args); got != 2 {
			t.Errorf("remoteWSLCLI(%v) = %d, want 2", args, got)
		}
	}
}

func TestRemoteWSLOffWindowsIsUnavailableNotFailed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("wsl.exe may be there")
	}
	if got := remoteWSLCLI([]string{"list"}); got != 3 {
		t.Fatalf("list = %d, want 3", got)
	}
}
