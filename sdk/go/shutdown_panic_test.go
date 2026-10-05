package extension

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestShutdownCallbackPanic(t *testing.T) {
	for _, mode := range []string{"bounded", "unbounded", "after-timeout"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestShutdownPanicProbeChild$", "-test.timeout=8s")
			cmd.Env = append(os.Environ(), "REASONIX_SDK_SHUTDOWN_PANIC_PROBE="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("shutdown probe failed: %v\n%s", err, output)
			}
		})
	}
}

type shutdownDiagnostic chan string

func (w shutdownDiagnostic) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func TestShutdownPanicProbeChild(t *testing.T) {
	mode := os.Getenv("REASONIX_SDK_SHUTDOWN_PANIC_PROBE")
	if mode == "" {
		t.Skip("subprocess probe")
	}
	timeout := 500
	if mode == "unbounded" {
		timeout = 0
	} else if mode == "after-timeout" {
		timeout = 20
	}
	release := make(chan struct{})
	diagnostics := make(shutdownDiagnostic, 4)
	host, serveDone := startFakeHost(t, basicHandler(), Options{
		Logger: log.New(diagnostics, "", 0),
		Shutdown: func(context.Context) {
			if mode == "after-timeout" {
				<-release
			}
			panic("shutdown-probe-panic")
		},
	})
	host.handshake(t)
	resp := host.request(MethodExtensionShutdown, ShutdownParams{TimeoutMillis: timeout})
	var result ShutdownResult
	if resp.Err != nil || json.Unmarshal(resp.Result, &result) != nil || !result.Accepted {
		t.Fatalf("shutdown acknowledgment = %s, error = %+v", resp.Result, resp.Err)
	}
	if err, ok := serveDone.wait(5 * time.Second); !ok || err != nil {
		t.Fatalf("Serve did not stop cleanly: error = %v, returned = %v", err, ok)
	}
	close(release)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		select {
		case line := <-diagnostics:
			if strings.Contains(line, "shutdown-probe-panic") {
				return
			}
		case <-deadline.C:
			t.Fatal("shutdown callback panic was not diagnosed")
		}
	}
}
