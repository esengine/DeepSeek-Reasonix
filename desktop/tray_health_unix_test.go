//go:build !windows && !darwin && cgo

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeStatusNotifierWatcher struct {
	mu    sync.RWMutex
	host  bool
	items []string
}

func (w *fakeStatusNotifierWatcher) set(host bool, items []string) {
	w.mu.Lock()
	w.host = host
	w.items = append([]string(nil), items...)
	w.mu.Unlock()
}

func (w *fakeStatusNotifierWatcher) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	if iface != statusNotifierWatcherIFace {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{iface})
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	switch property {
	case "IsStatusNotifierHostRegistered":
		return dbus.MakeVariant(w.host), nil
	case "RegisteredStatusNotifierItems":
		return dbus.MakeVariant(append([]string(nil), w.items...)), nil
	default:
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{property})
	}
}

func startPrivateDBusDaemon(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for the StatusNotifier integration test")
	}
	cmd := exec.Command(path, "--session", "--nofork", "--nopidfile", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-waitCh
		}
	})

	type addressResult struct {
		address string
		err     error
	}
	addressCh := make(chan addressResult, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		addressCh <- addressResult{address: strings.TrimSpace(line), err: readErr}
	}()
	select {
	case result := <-addressCh:
		if result.err != nil {
			t.Fatalf("read private dbus address: %v", result.err)
		}
		if result.address == "" {
			t.Fatal("private dbus daemon returned an empty address")
		}
		return result.address
	case <-time.After(5 * time.Second):
		t.Fatal("private dbus daemon did not publish its address")
		return ""
	}
}

func connectTestBus(t *testing.T, address string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func requestTestBusName(t *testing.T, conn *dbus.Conn, name string) {
	t.Helper()
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatal(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request %s: got reply %d", name, reply)
	}
}

func releaseTestBusName(t *testing.T, conn *dbus.Conn, name string) {
	t.Helper()
	reply, err := conn.ReleaseName(name)
	if err != nil {
		t.Fatal(err)
	}
	if reply != dbus.ReleaseNameReplyReleased {
		t.Fatalf("release %s: got reply %d", name, reply)
	}
}

func installFakeStatusNotifierWatcher(t *testing.T, conn *dbus.Conn, watcher *fakeStatusNotifierWatcher) {
	t.Helper()
	if err := conn.Export(watcher, statusNotifierWatcherPath, "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatal(err)
	}
	requestTestBusName(t, conn, statusNotifierWatcherName)
}

func TestStatusNotifierProbeIntegration(t *testing.T) {
	address := startPrivateDBusDaemon(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	const itemName = "org.kde.StatusNotifierItem-4242-1"

	probe := newStatusNotifierProbe()
	t.Cleanup(probe.close)
	assertState := func(wantReady bool, wantReason string) {
		t.Helper()
		ready, reason := probe.probe(context.Background(), itemName)
		if ready != wantReady || reason != wantReason {
			t.Fatalf("probe = (%v, %q), want (%v, %q)", ready, reason, wantReady, wantReason)
		}
	}

	assertState(false, "no_watcher")

	watcherConn := connectTestBus(t, address)
	watcher := &fakeStatusNotifierWatcher{}
	installFakeStatusNotifierWatcher(t, watcherConn, watcher)
	assertState(false, "no_host")

	watcher.set(true, []string{itemName + "/StatusNotifierItem"})
	assertState(false, "item_no_owner")

	itemConn := connectTestBus(t, address)
	requestTestBusName(t, itemConn, itemName)
	watcher.set(true, nil)
	assertState(false, "item_not_registered")

	watcher.set(true, []string{itemName + "/StatusNotifierItem"})
	assertState(true, "")

	watcher.set(false, []string{itemName + "/StatusNotifierItem"})
	assertState(false, "no_host")
	watcher.set(true, []string{itemName + "/StatusNotifierItem"})
	assertState(true, "")

	releaseTestBusName(t, watcherConn, statusNotifierWatcherName)
	assertState(false, "no_watcher")

	replacementConn := connectTestBus(t, address)
	replacement := &fakeStatusNotifierWatcher{}
	replacement.set(true, []string{itemName + "/StatusNotifierItem"})
	installFakeStatusNotifierWatcher(t, replacementConn, replacement)
	assertState(true, "")

	releaseTestBusName(t, itemConn, itemName)
	assertState(false, "item_no_owner")
	replacementItemConn := connectTestBus(t, address)
	requestTestBusName(t, replacementItemConn, itemName)
	assertState(true, "")
}

func startStallingDBusSocket(t *testing.T, serve func(*net.UnixConn) error) (string, <-chan error) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "bus.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.AcceptUnix()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer conn.Close()
		done <- serve(conn)
	}()
	return "unix:path=" + dbus.EscapeBusAddressValue(socketPath), done
}

func waitForConnectionResult(t *testing.T, resultCh <-chan error, serverDone <-chan error) {
	t.Helper()
	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("connect error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled DBus connection did not return")
	}
	select {
	case err := <-serverDone:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("fake DBus server: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled DBus connection did not close its transport")
	}
}

func TestConnectStatusNotifierSessionBusCancellationInterruptsAuth(t *testing.T) {
	accepted := make(chan struct{})
	address, serverDone := startStallingDBusSocket(t, func(conn *net.UnixConn) error {
		close(accepted)
		_, err := io.Copy(io.Discard, conn)
		return err
	})
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		connection, err := connectStatusNotifierSessionBus(ctx)
		if connection != nil {
			connection.close()
		}
		resultCh <- err
	}()
	select {
	case <-accepted:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("fake DBus server did not accept the connection")
	}
	waitForConnectionResult(t, resultCh, serverDone)
}

func serveUntilHello(conn *net.UnixConn, helloStarted chan<- struct{}) error {
	reader := bufio.NewReader(conn)
	first, err := reader.ReadByte()
	if err != nil {
		return err
	}
	if first != 0 {
		return fmt.Errorf("authentication prefix = %d, want 0", first)
	}
	readLine := func(wantPrefix string) error {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			return readErr
		}
		if !strings.HasPrefix(strings.TrimSpace(line), wantPrefix) {
			return fmt.Errorf("authentication line = %q, want prefix %q", line, wantPrefix)
		}
		return nil
	}
	writeLine := func(line string) error {
		_, writeErr := io.WriteString(conn, line+"\r\n")
		return writeErr
	}
	if err := readLine("AUTH"); err != nil {
		return err
	}
	if err := writeLine("REJECTED EXTERNAL"); err != nil {
		return err
	}
	if err := readLine("AUTH EXTERNAL"); err != nil {
		return err
	}
	if err := writeLine("OK 0123456789abcdef0123456789abcdef"); err != nil {
		return err
	}
	if err := readLine("NEGOTIATE_UNIX_FD"); err != nil {
		return err
	}
	if err := writeLine("ERROR"); err != nil {
		return err
	}
	if err := readLine("BEGIN"); err != nil {
		return err
	}
	if _, err := reader.ReadByte(); err != nil {
		return err
	}
	close(helloStarted)
	_, err = io.Copy(io.Discard, reader)
	return err
}

func TestConnectStatusNotifierSessionBusCancellationInterruptsHello(t *testing.T) {
	helloStarted := make(chan struct{})
	address, serverDone := startStallingDBusSocket(t, func(conn *net.UnixConn) error {
		return serveUntilHello(conn, helloStarted)
	})
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		connection, err := connectStatusNotifierSessionBus(ctx)
		if connection != nil {
			connection.close()
		}
		resultCh <- err
	}()
	select {
	case <-helloStarted:
		cancel()
	case err := <-serverDone:
		t.Fatalf("fake DBus server exited before Hello cancellation: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("client did not start the DBus Hello call")
	}
	waitForConnectionResult(t, resultCh, serverDone)
}
