//go:build windows

package instanceidentity

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestElectronEndpointUsesKernelProcessIdentityAndClosesGeneration(t *testing.T) {
	id := ForHome(t.TempDir())
	if got, err := EndpointImage(id); err != nil || got != "" {
		t.Fatalf("unstarted endpoint=%q %v", got, err)
	}
	stop, err := ListenEndpoint(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	if second, err := ListenEndpoint(id); err == nil {
		second()
		t.Fatal("allowed duplicate service endpoint")
	}
	got, err := EndpointImage(id)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatalf("endpoint owner=%s want=%s", got, want)
	}
	stop()
	if got, err := EndpointImage(id); err != nil || got != "" {
		t.Fatalf("closed generation=%q %v", got, err)
	}
	stopNext, err := ListenEndpoint(id)
	if err != nil {
		t.Fatal(err)
	}
	defer stopNext()
	stop() // old cleanup cannot close the new generation.
	if got, err := EndpointImage(id); err != nil || got == "" {
		t.Fatalf("replacement endpoint=%q %v", got, err)
	}
	other := ForHome(filepath.Join(t.TempDir(), "other"))
	if got, err := EndpointImage(other); err != nil || got != "" {
		t.Fatalf("cross-home endpoint=%q %v", got, err)
	}
}

func TestElectronEndpointShutdownCancelsUnacknowledgedClient(t *testing.T) {
	id := ForHome(t.TempDir())
	stop, err := ListenEndpoint(id)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	name, err := endpointName(id)
	if err != nil {
		t.Fatal(err)
	}
	client, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(client)
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked on a silent endpoint client")
	}
}
