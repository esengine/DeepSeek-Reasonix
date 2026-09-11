package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

func TestInstallIDConcurrentFirstUseGeneratesOneIdentity(t *testing.T) {
	isolateDesktopUserDirs(t)
	originalRandRead := installIDRandRead
	defer func() { installIDRandRead = originalRandRead }()

	firstGenerationStarted := make(chan struct{})
	releaseGeneration := make(chan struct{})
	var generations atomic.Int32
	installIDRandRead = func(p []byte) (int, error) {
		if generations.Add(1) == 1 {
			close(firstGenerationStarted)
			<-releaseGeneration
		}
		return rand.Read(p)
	}

	const callers = 16
	start := make(chan struct{})
	results := make(chan string, callers)
	errors := make(chan error, callers)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	for range callers {
		ready.Add(1)
		done.Go(func() {
			ready.Done()
			<-start
			id, err := installID()
			if err != nil {
				errors <- err
				return
			}
			results <- id
		})
	}
	ready.Wait()
	close(start)
	<-firstGenerationStarted
	close(releaseGeneration)
	done.Wait()
	close(results)
	close(errors)

	for err := range errors {
		t.Fatalf("installID: %v", err)
	}
	var canonical string
	for id := range results {
		if canonical == "" {
			canonical = id
		}
		if id != canonical {
			t.Fatalf("concurrent install IDs differ: got %q, want %q", id, canonical)
		}
	}
	if got := generations.Load(); got != 1 {
		t.Fatalf("identity generations = %d, want 1", got)
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
