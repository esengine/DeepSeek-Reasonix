//go:build !windows && !darwin && cgo

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	statusNotifierWatcherName  = "org.kde.StatusNotifierWatcher"
	statusNotifierWatcherPath  = dbus.ObjectPath("/StatusNotifierWatcher")
	statusNotifierWatcherIFace = "org.kde.StatusNotifierWatcher"
	statusNotifierPollInterval = time.Second
	statusNotifierProbeTimeout = 750 * time.Millisecond
)

type statusNotifierBusConnection struct {
	conn   *dbus.Conn
	cancel context.CancelFunc
}

func (c *statusNotifierBusConnection) close() {
	if c == nil {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

type statusNotifierProbe struct {
	connection *statusNotifierBusConnection
	connect    func(context.Context) (*statusNotifierBusConnection, error)
}

func newStatusNotifierProbe() *statusNotifierProbe {
	return &statusNotifierProbe{connect: connectStatusNotifierSessionBus}
}

func connectStatusNotifierSessionBus(ctx context.Context) (*statusNotifierBusConnection, error) {
	type connectionResult struct {
		conn *dbus.Conn
		err  error
	}

	connCtx, cancelConn := context.WithCancel(context.Background())
	resultCh := make(chan connectionResult, 1)
	go func() {
		conn, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(connCtx))
		if err == nil {
			err = conn.Auth(nil)
		}
		if err == nil {
			err = conn.Hello()
		}
		resultCh <- connectionResult{conn: conn, err: err}
	}()

	select {
	case result := <-resultCh:
		if err := ctx.Err(); err != nil {
			cancelConn()
			if result.conn != nil {
				_ = result.conn.Close()
			}
			return nil, err
		}
		if result.err != nil {
			cancelConn()
			if result.conn != nil {
				_ = result.conn.Close()
			}
			return nil, result.err
		}
		return &statusNotifierBusConnection{conn: result.conn, cancel: cancelConn}, nil
	case <-ctx.Done():
		// Canceling the connection context closes the private transport. That
		// interrupts Auth and Hello even though godbus does not expose
		// context-aware variants for those operations.
		cancelConn()
		return nil, ctx.Err()
	}
}

func (p *statusNotifierProbe) close() {
	if p == nil {
		return
	}
	p.connection.close()
	p.connection = nil
}

func (p *statusNotifierProbe) reset() {
	p.close()
}

func (p *statusNotifierProbe) probe(ctx context.Context, itemName string) (bool, string) {
	if p == nil {
		return false, "no_session_bus"
	}
	probeCtx, cancel := context.WithTimeout(ctx, statusNotifierProbeTimeout)
	defer cancel()
	if p.connection == nil {
		connect := p.connect
		if connect == nil {
			connect = connectStatusNotifierSessionBus
		}
		connection, err := connect(probeCtx)
		if err != nil {
			return false, "no_session_bus"
		}
		p.connection = connection
	}
	conn := p.connection.conn
	if err := conn.BusObject().CallWithContext(probeCtx, "org.freedesktop.DBus.Peer.Ping", 0).Err; err != nil {
		p.reset()
		return false, "no_session_bus"
	}
	snapshot, err := readStatusNotifierSnapshot(probeCtx, conn, itemName)
	if err != nil {
		return false, "watcher_unresponsive"
	}
	return evaluateStatusNotifierSnapshot(snapshot, itemName)
}

func readStatusNotifierSnapshot(ctx context.Context, conn *dbus.Conn, itemName string) (statusNotifierSnapshot, error) {
	snapshot := statusNotifierSnapshot{}
	bus := conn.Object("org.freedesktop.DBus", dbus.ObjectPath("/org/freedesktop/DBus"))
	if err := bus.CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, statusNotifierWatcherName).Store(&snapshot.WatcherOwner); err != nil {
		return snapshot, nil
	}
	if snapshot.WatcherOwner == "" {
		return snapshot, nil
	}
	watcher := conn.Object(statusNotifierWatcherName, statusNotifierWatcherPath)
	var value dbus.Variant
	if err := watcher.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, statusNotifierWatcherIFace, "IsStatusNotifierHostRegistered").Store(&value); err != nil {
		return snapshot, err
	}
	snapshot.Host, _ = value.Value().(bool)
	if err := watcher.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, statusNotifierWatcherIFace, "RegisteredStatusNotifierItems").Store(&value); err != nil {
		return snapshot, err
	}
	snapshot.Items, _ = value.Value().([]string)
	_ = bus.CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, itemName).Store(&snapshot.ItemOwner)
	return snapshot, nil
}

func (a *App) startTrayHealthMonitor(t *desktopTray) {
	if a == nil || t == nil {
		return
	}
	ctx, cancel := context.WithCancel(a.bootContext())
	t.healthMu.Lock()
	if t.healthStopped {
		t.healthMu.Unlock()
		cancel()
		return
	}
	t.cancel = cancel
	t.healthMu.Unlock()
	a.goSafe("trayStatusNotifierMonitor", func() {
		itemName := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
		probe := newStatusNotifierProbe()
		defer probe.close()

		for {
			ready, reason := probe.probe(ctx, itemName)
			if ready {
				a.setTrayHealth(t, "ready", "")
			} else {
				a.setTrayHealth(t, "unavailable", reason)
			}
			timer := time.NewTimer(statusNotifierPollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})
}

// Linux readiness is established by the DBus monitor, not systray.onReady.
func (a *App) trayConfigured(*desktopTray) {}
