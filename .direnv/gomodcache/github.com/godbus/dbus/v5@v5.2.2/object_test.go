package dbus

import (
	"context"
	"testing"
	"time"
)

type objectGoContextServer struct {
	t     *testing.T
	sleep time.Duration
}

func (o objectGoContextServer) Sleep() *Error {
	o.t.Log("Got object call and sleeping for ", o.sleep)
	time.Sleep(o.sleep)
	o.t.Log("Completed sleeping for ", o.sleep)
	return nil
}

func TestObjectGoWithContextTimeout(t *testing.T) {
	bus, err := ConnectSessionBus()
	if err != nil {
		t.Fatalf("Unexpected error connecting to session bus: %s", err)
	}
	defer bus.Close()

	name := bus.Names()[0]
	err = bus.Export(objectGoContextServer{t, time.Second}, "/org/dannin/DBus/Test", "org.dannin.DBus.Test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case call := <-bus.Object(name, "/org/dannin/DBus/Test").GoWithContext(ctx, "org.dannin.DBus.Test.Sleep", 0, nil).Done:
		if call.Err != ctx.Err() {
			t.Fatal("Expected ", ctx.Err(), " but got ", call.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Expected call to not respond in time")
	}
}

func TestObjectGoWithContext(t *testing.T) {
	bus, err := ConnectSessionBus()
	if err != nil {
		t.Fatalf("Unexpected error connecting to session bus: %s", err)
	}
	defer bus.Close()

	name := bus.Names()[0]
	err = bus.Export(objectGoContextServer{t, time.Millisecond}, "/org/dannin/DBus/Test", "org.dannin.DBus.Test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case call := <-bus.Object(name, "/org/dannin/DBus/Test").GoWithContext(ctx, "org.dannin.DBus.Test.Sleep", 0, nil).Done:
		if call.Err != ctx.Err() {
			t.Fatal("Expected ", ctx.Err(), " but got ", call.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("Expected call to respond in 1 Millisecond")
	}
}

type nopServer struct{}

func (nopServer) Nop() *Error {
	return nil
}

func TestObjectSignalHandling(t *testing.T) {
	bus, err := ConnectSessionBus()
	if err != nil {
		t.Fatalf("Unexpected error connecting to session bus: %s", err)
	}
	defer bus.Close()

	name := bus.Names()[0]
	path := ObjectPath("/org/godbus/DBus/TestSignals")
	otherPath := ObjectPath("/org/other/godbus/DBus/TestSignals")
	iface := "org.godbus.DBus.TestSignals"
	otherIface := "org.godbus.DBus.OtherTestSignals"
	err = bus.Export(nopServer{}, path, iface)
	if err != nil {
		t.Fatalf("Unexpected error registering nop server: %v", err)
	}

	obj := bus.Object(name, path)
	if err := bus.AddMatchSignal(
		WithMatchInterface(iface),
		WithMatchMember("Heartbeat"),
		WithMatchObjectPath(path),
	); err != nil {
		t.Fatal(err)
	}

	ch := make(chan *Signal, 5)
	bus.Signal(ch)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				t.Errorf("Caught panic in emitter goroutine: %v", err)
			}
		}()

		emit := func(path ObjectPath, name string, values ...any) {
			t.Helper()
			if err := bus.Emit(path, name, values...); err != nil {
				t.Error("Emit:", err)
			}
		}

		// desired signals
		emit(path, iface+".Heartbeat", uint32(1))
		emit(path, iface+".Heartbeat", uint32(2))
		// undesired signals
		emit(otherPath, iface+".Heartbeat", uint32(3))
		emit(otherPath, otherIface+".Heartbeat", uint32(4))
		emit(path, iface+".Updated", false)
		// sentinel
		emit(path, iface+".Heartbeat", uint32(5))

		time.Sleep(100 * time.Millisecond)
		emit(path, iface+".Heartbeat", uint32(6))
	}()

	checkSignal := func(ch chan *Signal, value uint32) {
		t.Helper()

		const timeout = 50 * time.Millisecond
		var sig *Signal

		select {
		case sig = <-ch:
			// do nothing
		case <-time.After(timeout):
			t.Fatalf("Failed to fetch signal in specified timeout %s", timeout)
		}

		if sig.Path != path {
			t.Errorf("signal.Path mismatch: %s != %s", path, sig.Path)
		}

		name := iface + ".Heartbeat"
		if sig.Name != name {
			t.Errorf("signal.Name mismatch: %s != %s", name, sig.Name)
		}

		if len(sig.Body) != 1 {
			t.Errorf("Invalid signal body length: %d", len(sig.Body))
			return
		}

		if sig.Body[0] != any(value) {
			t.Errorf("signal value mismatch: %d != %d", value, sig.Body[0])
		}
	}

	checkSignal(ch, 1)
	checkSignal(ch, 2)
	checkSignal(ch, 5)

	obj.RemoveMatchSignal(iface, "Heartbeat", WithMatchObjectPath(obj.Path()))
	select {
	case sig := <-ch:
		t.Errorf("Got signal after removing subscription: %v", sig)
	case <-time.After(200 * time.Millisecond):
	}
}
