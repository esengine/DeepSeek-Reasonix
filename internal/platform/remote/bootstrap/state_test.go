package bootstrap

import (
	"strings"
	"testing"
)

func TestServeStateRoundTrip(t *testing.T) {
	st := ServeState{
		PID:       1234,
		Addr:      "127.0.0.1:38121",
		Workspace: "/home/dev/app",
		Version:   "1.9.0",
		TokenFile: "/home/dev/.reasonix/remote/serve-x.token",
		LogFile:   "/home/dev/.reasonix/remote/serve-x.log",
		StartedAt: 1_700_000_000,
	}
	data, err := MarshalState(st)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalState(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != st {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, st)
	}
}

// TestServeStateBackwardCompat: an older record missing the newer optional
// fields must decode without error, leaving zero values.
func TestServeStateBackwardCompat(t *testing.T) {
	old := `{"pid": 42, "addr": "127.0.0.1:9000", "workspace": "/app", "token_file": "/t"}`
	st, err := UnmarshalState([]byte(old))
	if err != nil {
		t.Fatalf("old record failed to decode: %v", err)
	}
	if st.PID != 42 || st.Addr != "127.0.0.1:9000" || st.Version != "" || st.StartedAt != 0 {
		t.Fatalf("backward-compat decode wrong: %+v", st)
	}
}

func TestPathsFor(t *testing.T) {
	paths := pathsFor("/home/dev", "/home/dev/projects/app")
	if paths.Dir != "/home/dev/.reasonix/remote" {
		t.Fatalf("Dir = %q", paths.Dir)
	}
	if !strings.HasPrefix(paths.StateJSON, paths.Dir+"/serve-") || !strings.HasSuffix(paths.StateJSON, ".json") {
		t.Fatalf("StateJSON = %q", paths.StateJSON)
	}
	if !strings.HasSuffix(paths.TokenFile, ".token") || !strings.HasSuffix(paths.PortFile, ".port") {
		t.Fatalf("token/port names wrong: %+v", paths)
	}
}
