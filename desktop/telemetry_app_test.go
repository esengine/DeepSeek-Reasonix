package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstallIDStableAcrossCalls(t *testing.T) {
	isolateDesktopUserDirs(t)
	first, err := installID()
	if err != nil {
		t.Fatal(err)
	}
	if !installIDPattern.MatchString(first) {
		t.Fatalf("installID() = %q, want 32 hex chars", first)
	}
	second, err := installID()
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("second call returned %q, want stable %q", second, first)
	}
}

func TestPostStartupPing(t *testing.T) {
	var got startupPing
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body not JSON: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := startupPing{InstallID: "0123456789abcdef0123456789abcdef", Version: "v9.9.9", OS: "windows", Arch: "amd64", OSVersion: "Windows 10.0 build 26200"}
	if err := postStartupPing(context.Background(), srv.Client(), srv.URL, p); err != nil {
		t.Fatal(err)
	}
	if got != p {
		t.Errorf("server received %+v, want %+v", got, p)
	}
}

func TestSendStartupPingSkipsDevBuild(t *testing.T) {
	isolateDesktopUserDirs(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	old := pingEndpoint
	pingEndpoint = srv.URL
	defer func() { pingEndpoint = old }()

	NewApp().sendStartupPing()
	if hits != 0 {
		t.Errorf("dev build sent %d pings, want 0", hits)
	}
}
