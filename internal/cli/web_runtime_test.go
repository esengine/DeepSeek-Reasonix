package cli

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestServeConfigWithCommandDefaults(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		authExplicit bool
		configured   string
		want         string
	}{
		{name: "web generates token by default", command: "web", want: "token"},
		{name: "web overrides configured none by default", command: "web", configured: "none", want: "token"},
		{name: "web explicit auth wins", command: "web", authExplicit: true, configured: "none", want: "none"},
		{name: "serve stays config driven", command: "serve", configured: "password", want: "password"},
		{name: "serve empty stays backward compatible", command: "serve", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serveConfigWithCommandDefaults(tt.command, tt.authExplicit, config.ServeConfig{AuthMode: tt.configured})
			if got.AuthMode != tt.want {
				t.Fatalf("AuthMode = %q, want %q", got.AuthMode, tt.want)
			}
		})
	}
}

func TestListenWebWithPortRetryUsesNextAvailablePort(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	base := occupied.Addr().(*net.TCPAddr).Port
	if base == 65535 {
		t.Skip("ephemeral allocation left no higher port")
	}

	ln, err := listenWebWithPortRetry(net.JoinHostPort("127.0.0.1", strconv.Itoa(base)))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := ln.Addr().(*net.TCPAddr).Port
	if got <= base || got > base+webPortRetryLimit+1 {
		t.Fatalf("bound port = %d, want a higher port near occupied %d", got, base)
	}
}

func TestListenWebWithPortRetryPreservesEphemeralPort(t *testing.T) {
	ln, err := listenWebWithPortRetry("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if got := ln.Addr().(*net.TCPAddr).Port; got == 0 {
		t.Fatal("kernel did not assign an ephemeral port")
	}
}

func TestValidateWebSessionID(t *testing.T) {
	for _, valid := range []string{"20260809-122436.032610000-deepseek-v4-flash", "session with space", "a.b-c_d"} {
		if err := validateWebSessionID(valid); err != nil {
			t.Errorf("validateWebSessionID(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", " ", ".", "..", "a/b", `a\b`, "thing.events"} {
		if err := validateWebSessionID(invalid); err == nil {
			t.Errorf("validateWebSessionID(%q) succeeded", invalid)
		}
	}
}

func TestFreshWebSessionPathKeepsReservedIdentityWithoutMaterializing(t *testing.T) {
	dir := t.TempDir()
	got, err := freshWebSessionPath(dir, "reserved-session")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "reserved-session.jsonl")
	if got != want {
		t.Fatalf("fresh path = %q, want %q", got, want)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Fatalf("fresh identity should stay lazy on disk, stat error = %v", err)
	}
	if err := os.WriteFile(got, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := freshWebSessionPath(dir, "reserved-session"); err == nil {
		t.Fatal("existing transcript was accepted as a fresh Web identity")
	}
}

func TestWebInstanceRegistryPreservesIndependentInstances(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server", "instances")
	now := time.UnixMilli(1000)
	alive := map[int]bool{101: true, 202: true}
	registry := &webInstanceRegistry{
		dir:               dir,
		now:               func() time.Time { now = now.Add(time.Millisecond); return now },
		heartbeatInterval: 0,
		processAlive:      func(pid int) bool { return alive[pid] },
	}
	first, err := registry.register("127.0.0.1:8787", 101)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Release)
	second, err := registry.register("127.0.0.1:8788", 202)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Release)
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("registry directory mode = %o, want 700", dirInfo.Mode().Perm())
	}

	live, err := registry.listLive()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 2 || live[0].Port != 8787 || live[1].Port != 8788 {
		t.Fatalf("live instances = %+v, want ports 8787 and 8788", live)
	}
	for _, reg := range []*webInstanceRegistration{first, second} {
		info, err := os.Stat(reg.path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("instance mode = %o, want 600", info.Mode().Perm())
		}
	}

	first.Release()
	live, err = registry.listLive()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].PID != 202 {
		t.Fatalf("live after releasing first = %+v, want only second", live)
	}
}

func TestWebInstanceRegistrySweepsOnlyConfirmedDeadRecords(t *testing.T) {
	dir := t.TempDir()
	deadPath := filepath.Join(dir, "dead.json")
	dead := webInstanceRecord{ServerID: "dead", PID: 303, Host: "127.0.0.1", Port: 8787, StartedAt: 1, HeartbeatAt: 1}
	data, err := json.Marshal(dead)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deadPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	garbagePath := filepath.Join(dir, "future.json")
	if err := os.WriteFile(garbagePath, []byte(`{"future_schema":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := &webInstanceRegistry{
		dir:          dir,
		processAlive: func(int) bool { return false },
	}
	if err := registry.sweepStale(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Fatalf("dead record still exists: %v", err)
	}
	if _, err := os.Stat(garbagePath); err != nil {
		t.Fatalf("unparseable future record should be preserved: %v", err)
	}
}
