package dbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"sync"
	"testing"
	"time"
)

func TestSessionBus(t *testing.T) {
	oldConn, err := SessionBus()
	if err != nil {
		t.Error(err)
	}
	if err = oldConn.Close(); err != nil {
		t.Fatal(err)
	}
	if oldConn.Connected() {
		t.Fatal("Should be closed")
	}
	newConn, err := SessionBus()
	if err != nil {
		t.Error(err)
	}
	if newConn == oldConn {
		t.Fatal("Should get a new connection")
	}
}

func TestSystemBus(t *testing.T) {
	oldConn, err := SystemBus()
	if err != nil {
		t.Error(err)
	}
	if err = oldConn.Close(); err != nil {
		t.Fatal(err)
	}
	if oldConn.Connected() {
		t.Fatal("Should be closed")
	}
	newConn, err := SystemBus()
	if err != nil {
		t.Error(err)
	}
	if newConn == oldConn {
		t.Fatal("Should get a new connection")
	}
}

func TestConnectSessionBus(t *testing.T) {
	conn, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.Connected() {
		t.Fatal("Should be closed")
	}
}

func TestConnectSystemBus(t *testing.T) {
	conn, err := ConnectSystemBus()
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.Connected() {
		t.Fatal("Should be closed")
	}
}

func TestSend(t *testing.T) {
	bus, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	ch := make(chan *Call, 1)
	msg := &Message{
		Type:  TypeMethodCall,
		Flags: 0,
		Headers: map[HeaderField]Variant{
			FieldDestination: MakeVariant(bus.Names()[0]),
			FieldPath:        MakeVariant(ObjectPath("/org/freedesktop/DBus")),
			FieldInterface:   MakeVariant("org.freedesktop.DBus.Peer"),
			FieldMember:      MakeVariant("Ping"),
		},
	}
	call := bus.Send(msg, ch)
	<-ch
	if call.Err != nil {
		t.Error(call.Err)
	}
}

func TestFlagNoReplyExpectedSend(t *testing.T) {
	bus, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	done := make(chan struct{})
	go func() {
		bus.BusObject().Call("org.freedesktop.DBus.ListNames", FlagNoReplyExpected)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("Failed to announce that the call was done")
	}
}

func TestRemoveSignal(t *testing.T) {
	bus, err := NewConn(nil)
	if err != nil {
		t.Error(err)
	}
	ch := make(chan *Signal)
	ch2 := make(chan *Signal)
	for _, ch := range []chan *Signal{ch, ch2, ch, ch2, ch2, ch} {
		bus.Signal(ch)
	}
	signals := bus.signalHandler.(*defaultSignalHandler).signals
	if len(signals) != 6 {
		t.Errorf("remove signal: signals length not equal: got '%d', want '6'", len(signals))
	}
	bus.RemoveSignal(ch)
	signals = bus.signalHandler.(*defaultSignalHandler).signals
	if len(signals) != 3 {
		t.Errorf("remove signal: signals length not equal: got '%d', want '3'", len(signals))
	}
	signals = bus.signalHandler.(*defaultSignalHandler).signals
	for _, scd := range signals {
		if scd.ch != ch2 {
			t.Errorf("remove signal: removed signal present: got '%v', want '%v'", scd.ch, ch2)
		}
	}
}

type rwc struct {
	io.Reader
	io.Writer
}

func (rwc) Close() error { return nil }

type fakeAuth struct{}

func (fakeAuth) FirstData() (name, resp []byte, status AuthStatus) {
	return []byte("name"), []byte("resp"), AuthOk
}

func (fakeAuth) HandleData(data []byte) (resp []byte, status AuthStatus) {
	return nil, AuthOk
}

func TestCloseBeforeSignal(t *testing.T) {
	reader, pipewriter := io.Pipe()
	defer pipewriter.Close()
	defer reader.Close()

	bus, err := NewConn(rwc{Reader: reader, Writer: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	// give ch a buffer so sends won't block
	ch := make(chan *Signal, 1)
	bus.Signal(ch)

	go func() {
		_, err := pipewriter.Write([]byte("REJECTED name\r\nOK myuuid\r\n"))
		if err != nil {
			t.Errorf("error writing to pipe: %v", err)
		}
	}()

	err = bus.Auth([]Auth{fakeAuth{}})
	if err != nil {
		t.Fatal(err)
	}

	err = bus.Close()
	if err != nil {
		t.Fatal(err)
	}

	msg := &Message{
		Type: TypeSignal,
		Headers: map[HeaderField]Variant{
			FieldInterface: MakeVariant("foo.bar"),
			FieldMember:    MakeVariant("bar"),
			FieldPath:      MakeVariant(ObjectPath("/baz")),
		},
	}
	err = msg.EncodeTo(pipewriter, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCloseChannelAfterRemoveSignal(t *testing.T) {
	bus, err := NewConn(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Add an unbuffered signal channel
	ch := make(chan *Signal)
	bus.Signal(ch)

	// Send a signal
	msg := &Message{
		Type: TypeSignal,
		Headers: map[HeaderField]Variant{
			FieldInterface: MakeVariant("foo.bar"),
			FieldMember:    MakeVariant("bar"),
			FieldPath:      MakeVariant(ObjectPath("/baz")),
		},
	}
	bus.handleSignal(Sequence(1), msg)

	// Remove and close the signal channel
	bus.RemoveSignal(ch)
	close(ch)
}

func TestAddAndRemoveMatchSignalContext(t *testing.T) {
	conn, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sigc := make(chan *Signal, 1)
	conn.Signal(sigc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// try to subscribe to a made up signal with an already canceled context
	if err = conn.AddMatchSignalContext(
		ctx,
		WithMatchInterface("org.test"),
		WithMatchMember("CtxTest"),
	); err == nil {
		t.Fatal("call on canceled context did not fail")
	}

	// subscribe to the signal with background context
	if err = conn.AddMatchSignalContext(
		context.Background(),
		WithMatchInterface("org.test"),
		WithMatchMember("CtxTest"),
	); err != nil {
		t.Fatal(err)
	}

	// try to unsubscribe with an already canceled context
	if err = conn.RemoveMatchSignalContext(
		ctx,
		WithMatchInterface("org.test"),
		WithMatchMember("CtxTest"),
	); err == nil {
		t.Fatal("call on canceled context did not fail")
	}

	// check that signal is still delivered
	if err = conn.Emit("/", "org.test.CtxTest"); err != nil {
		t.Fatal(err)
	}
	if sig := waitSignal(sigc, "org.test.CtxTest"); sig == nil {
		t.Fatal("signal receive timed out")
	}

	// unsubscribe from the signal
	if err = conn.RemoveMatchSignalContext(
		context.Background(),
		WithMatchInterface("org.test"),
		WithMatchMember("CtxTest"),
	); err != nil {
		t.Fatal(err)
	}
	if err = conn.Emit("/", "org.test.CtxTest"); err != nil {
		t.Fatal(err)
	}
	if sig := waitSignal(sigc, "org.test.CtxTest"); sig != nil {
		t.Fatalf("unsubscribed from %q signal, but received %#v", "org.test.CtxTest", sig)
	}
}

func TestAddAndRemoveMatchSignal(t *testing.T) {
	conn, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sigc := make(chan *Signal, 1)
	conn.Signal(sigc)

	// subscribe to a made up signal name and emit one of the type
	if err = conn.AddMatchSignal(
		WithMatchInterface("org.test"),
		WithMatchMember("Test"),
	); err != nil {
		t.Fatal(err)
	}
	if err = conn.Emit("/", "org.test.Test"); err != nil {
		t.Fatal(err)
	}
	if sig := waitSignal(sigc, "org.test.Test"); sig == nil {
		t.Fatal("signal receive timed out")
	}

	// unsubscribe from the signal and check that is not delivered anymore
	if err = conn.RemoveMatchSignal(
		WithMatchInterface("org.test"),
		WithMatchMember("Test"),
	); err != nil {
		t.Fatal(err)
	}
	if err = conn.Emit("/", "org.test.Test"); err != nil {
		t.Fatal(err)
	}
	if sig := waitSignal(sigc, "org.test.Test"); sig != nil {
		t.Fatalf("unsubscribed from %q signal, but received %#v", "org.test.Test", sig)
	}
}

func waitSignal(sigc <-chan *Signal, name string) *Signal {
	for {
		select {
		case sig := <-sigc:
			if sig.Name == name {
				return sig
			}
		case <-time.After(time.Second):
			return nil
		}
	}
}

const (
	SCPPInterface         = "org.godbus.DBus.StatefulTest"
	SCPPPath              = "/org/godbus/DBus/StatefulTest"
	SCPPChangedSignalName = "Changed"
	SCPPStateMethodName   = "State"
)

func TestStateCachingProxyPattern(t *testing.T) {
	srv, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	conn, err := ConnectSessionBus(WithSignalHandler(NewSequentialSignalHandler()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	serviceName := srv.Names()[0]
	// message channel should have at least some buffering, to make sure Eavesdrop does not
	// drop the message if nobody is currently trying to read from the channel.
	messages := make(chan *Message, 1)
	srv.Eavesdrop(messages)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := serverProcess(ctx, srv, messages, t); err != nil {
			t.Errorf("error in server process: %v", err)
			cancel()
		}
	}()
	go func() {
		defer wg.Done()
		if err := clientProcess(ctx, conn, serviceName, t); err != nil {
			t.Errorf("error in client process: %v", err)
		}
		// Cancel the server process.
		cancel()
	}()
	wg.Wait()
}

func clientProcess(ctx context.Context, conn *Conn, serviceName string, t *testing.T) error {
	// Subscribe to state changes on the remote object
	if err := conn.AddMatchSignal(
		WithMatchInterface(SCPPInterface),
		WithMatchMember(SCPPChangedSignalName),
	); err != nil {
		return err
	}
	channel := make(chan *Signal)
	conn.Signal(channel)
	t.Log("Subscribed to signals")

	// Simulate unfavourable OS scheduling leading to a delay between subscription
	// and querying for the current state.
	time.Sleep(30 * time.Millisecond)

	// Call .State() on the remote object to get its current state and store in observedStates[0].
	obj := conn.Object(serviceName, SCPPPath)
	observedStates := make([]uint64, 1)
	call := obj.CallWithContext(ctx, SCPPInterface+"."+SCPPStateMethodName, 0)
	if err := call.Store(&observedStates[0]); err != nil {
		return err
	}
	t.Logf("Queried current state, got %v", observedStates[0])

	// Populate observedStates[1...49] based on the state change signals,
	// ignoring signals with a sequence number less than call.ResponseSequence so that we ignore past signals.
	signalsProcessed := 0
readSignals:
	for {
		select {
		case signal := <-channel:
			signalsProcessed++
			if signal.Name == SCPPInterface+"."+SCPPChangedSignalName && signal.Sequence > call.ResponseSequence {
				observedState := signal.Body[0].(uint64)
				observedStates = append(observedStates, observedState)
				// Observing at least 50 states gives us low probability that we received a contiguous subsequence of states 'by accident'
				if len(observedStates) >= 50 {
					break readSignals
				}
			}
		case <-ctx.Done():
			t.Logf("Context cancelled, client processed %v signals", signalsProcessed)
			return ctx.Err()
		}
	}
	t.Logf("client processed %v signals", signalsProcessed)

	// Expect that we begun observing at least a few states in. This ensures the server was already emitting signals
	// and makes it likely we simulated our race condition.
	if observedStates[0] < 10 {
		return fmt.Errorf("expected first state to be at least 10, got %v", observedStates[0])
	}

	t.Logf("Observed states: %v", observedStates)

	// The observable states of the remote object were [1 ... (infinity)] during this test.
	// This loop is intended to assert that our observed states are a contiguous subgrange [n ... n+49] for some n, i.e.
	// that we received a contiguous subsequence of the states of the remote object. For each run of the test, n
	// may be slightly different due to scheduling effects.
	for i := 0; i < len(observedStates); i++ {
		expectedState := observedStates[0] + uint64(i)
		if observedStates[i] != expectedState {
			return fmt.Errorf("expected observed state %v to be %v, got %v", i, expectedState, observedStates[i])
		}
	}
	return nil
}

func serverProcess(ctx context.Context, srv *Conn, messages <-chan *Message, t *testing.T) error {
	state := uint64(0)

process:
	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				t.Log("Message channel closed")
				// Message channel closed.
				break process
			}
			if msg.IsValid() != nil {
				t.Log("Got invalid message, discarding")
				continue process
			}
			name := msg.Headers[FieldMember].value.(string)
			ifname := msg.Headers[FieldInterface].value.(string)
			if ifname == SCPPInterface && name == SCPPStateMethodName {
				t.Logf("Processing reply to .State(), returning state = %v", state)
				reply := new(Message)
				reply.Type = TypeMethodReply
				reply.Headers = make(map[HeaderField]Variant)
				reply.Headers[FieldDestination] = msg.Headers[FieldSender]
				reply.Headers[FieldReplySerial] = MakeVariant(msg.serial)
				reply.Body = make([]any, 1)
				reply.Body[0] = state
				reply.Headers[FieldSignature] = MakeVariant(SignatureOf(reply.Body...))
				_ = srv.sendMessageAndIfClosed(reply, nil)
			}
		case <-ctx.Done():
			t.Logf("Context cancelled, server emitted %v signals", state)
			return nil
		default:
			state++
			if err := srv.Emit(SCPPPath, SCPPInterface+"."+SCPPChangedSignalName, state); err != nil {
				return err
			}
		}
	}
	return nil
}

type server struct{}

func (server) Double(i int64) (int64, *Error) {
	return 2 * i, nil
}

func BenchmarkCall(b *testing.B) {
	b.StopTimer()
	b.ReportAllocs()
	var s string
	bus, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	name := bus.Names()[0]
	obj := bus.BusObject()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err := obj.Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&s)
		if err != nil {
			b.Fatal(err)
		}
		if s != name {
			b.Errorf("got %s, wanted %s", s, name)
		}
	}
}

func BenchmarkCallAsync(b *testing.B) {
	b.StopTimer()
	b.ReportAllocs()
	bus, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	name := bus.Names()[0]
	obj := bus.BusObject()
	c := make(chan *Call, 50)
	done := make(chan struct{})
	go func() {
		for i := 0; i < b.N; i++ {
			v := <-c
			if v.Err != nil {
				b.Error(v.Err)
			}
			s := v.Body[0].(string)
			if s != name {
				b.Errorf("got %s, wanted %s", s, name)
			}
		}
		close(done)
	}()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		obj.Go("org.freedesktop.DBus.GetNameOwner", 0, c, name)
	}
	<-done
}

func BenchmarkServe(b *testing.B) {
	b.StopTimer()
	srv, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer srv.Close()

	cli, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer cli.Close()

	benchmarkServe(b, srv, cli)
}

func BenchmarkServeAsync(b *testing.B) {
	b.StopTimer()
	srv, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer srv.Close()

	cli, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer cli.Close()

	benchmarkServeAsync(b, srv, cli)
}

func BenchmarkServeSameConn(b *testing.B) {
	b.StopTimer()
	bus, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	benchmarkServe(b, bus, bus)
}

func BenchmarkServeSameConnAsync(b *testing.B) {
	b.StopTimer()
	bus, err := ConnectSessionBus()
	if err != nil {
		b.Fatal(err)
	}
	defer bus.Close()

	benchmarkServeAsync(b, bus, bus)
}

func benchmarkServe(b *testing.B, srv, cli *Conn) {
	var r int64

	err := srv.Export(server{}, "/org/guelfey/DBus/Test", "org.guelfey.DBus.Test")
	if err != nil {
		b.Fatal(err)
	}

	dest := srv.Names()[0]
	obj := cli.Object(dest, "/org/guelfey/DBus/Test")
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err = obj.Call("org.guelfey.DBus.Test.Double", 0, int64(i)).Store(&r)
		if err != nil {
			b.Fatal(err)
		}
		if r != 2*int64(i) {
			b.Errorf("got %d, wanted %d", r, 2*int64(i))
		}
	}
}

func benchmarkServeAsync(b *testing.B, srv, cli *Conn) {
	dest := srv.Names()[0]
	err := srv.Export(server{}, "/org/guelfey/DBus/Test", "org.guelfey.DBus.Test")
	if err != nil {
		b.Fatal(err)
	}
	obj := cli.Object(dest, "/org/guelfey/DBus/Test")
	c := make(chan *Call, 50)
	done := make(chan struct{})
	go func() {
		for i := 0; i < b.N; i++ {
			v := <-c
			if v.Err != nil {
				b.Error(v.Err)
				return
			}
			i, r := v.Args[0].(int64), v.Body[0].(int64)
			if 2*i != r {
				b.Errorf("got %d, wanted %d", r, 2*i)
			}
		}
		close(done)
	}()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		obj.Go("org.guelfey.DBus.Test.Double", 0, c, int64(i))
	}
	<-done
}

func TestGetKey(t *testing.T) {
	keys := "host=1.2.3.4,port=5678,family=ipv4"
	if host := getKey(keys, "host"); host != "1.2.3.4" {
		t.Error(`Expected "1.2.3.4", got`, host)
	}
	if port := getKey(keys, "port"); port != "5678" {
		t.Error(`Expected "5678", got`, port)
	}
	if family := getKey(keys, "family"); family != "ipv4" {
		t.Error(`Expected "ipv4", got`, family)
	}
}

func TestInterceptors(t *testing.T) {
	conn, err := ConnectSessionBus(
		WithIncomingInterceptor(func(msg *Message) {
			log.Println("<", msg)
		}),
		WithOutgoingInterceptor(func(msg *Message) {
			log.Println(">", msg)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
}

func TestCloseCancelsConnectionContext(t *testing.T) {
	bus, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	// The context is not done at this point
	ctx := bus.Context()
	select {
	case <-ctx.Done():
		t.Fatal("context should not be done")
	default:
	}

	err = bus.Close()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		// expected
	case <-time.After(5 * time.Second):
		t.Fatal("context is not done after connection closed")
	}
}

func TestDisconnectCancelsConnectionContext(t *testing.T) {
	reader, pipewriter := io.Pipe()
	defer pipewriter.Close()
	defer reader.Close()

	bus, err := NewConn(rwc{Reader: reader, Writer: io.Discard})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		_, err := pipewriter.Write([]byte("REJECTED name\r\nOK myuuid\r\n"))
		if err != nil {
			t.Errorf("error writing to pipe: %v", err)
		}
	}()
	err = bus.Auth([]Auth{fakeAuth{}})
	if err != nil {
		t.Fatal(err)
	}

	ctx := bus.Context()

	pipewriter.Close()
	select {
	case <-ctx.Done():
		// expected
	case <-time.After(5 * time.Second):
		t.Fatal("context is not done after connection closed")
	}
}

func TestCancellingContextClosesConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader, pipewriter := io.Pipe()
	defer pipewriter.Close()
	defer reader.Close()

	bus, err := NewConn(rwc{Reader: reader, Writer: io.Discard}, WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		_, err := pipewriter.Write([]byte("REJECTED name\r\nOK myuuid\r\n"))
		if err != nil {
			t.Errorf("error writing to pipe: %v", err)
		}
	}()
	err = bus.Auth([]Auth{fakeAuth{}})
	if err != nil {
		t.Fatal(err)
	}

	// Cancel the connection's parent context and give time for
	// other goroutines to schedule.
	cancel()
	time.Sleep(50 * time.Millisecond)

	err = bus.BusObject().Call("org.freedesktop.DBus.Peer.Ping", 0).Store()
	if err != ErrClosed {
		t.Errorf("expected connection to be closed, but got: %v", err)
	}
}

// TestTimeoutContextClosesConnection checks that a Conn instance is closed after
// the passed context's deadline is missed.
// The test also checks that there's no data race between Conn creation and its
// automatic closing.
func TestTimeoutContextClosesConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	bus, err := NewConn(rwc{}, WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}

	// wait until the connection is actually closed
	time.Sleep(50 * time.Millisecond)

	err = bus.BusObject().Call("org.freedesktop.DBus.Peer.Ping", 0).Store()
	if err != ErrClosed {
		t.Errorf("expected connection to be closed, but got: %v", err)
	}
}
