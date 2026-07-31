package dbus

import (
	"context"
	"testing"
	"time"
)

// Verifies that no signals are dropped, even if there is not enough space
// in the destination channel.
func TestSequentialHandlerNoDrop(t *testing.T) {
	t.Parallel()

	handler := NewSequentialSignalHandler()

	channel := make(chan *Signal, 2)
	handler.(SignalRegistrar).AddSignal(channel)

	writeSignals(handler, 1000)

	readSignals(t, channel)
}

// Verifies that signals are written to the destination channel in the
// order they are received, in a typical concurrent reader/writer scenario.
func TestSequentialHandlerSequential(t *testing.T) {
	t.Parallel()

	handler := NewSequentialSignalHandler()

	channel := make(chan *Signal, 10)
	handler.(SignalRegistrar).AddSignal(channel)

	done := make(chan struct{})

	// Concurrently read and write signals
	go func() {
		readSignals(t, channel)
		close(done)
	}()
	writeSignals(handler, 1000)
	<-done
}

// Test that in the case of multiple destination channels, one channel
// being blocked does not prevent the other channel receiving messages.
func TestSequentialHandlerMultipleChannel(t *testing.T) {
	t.Parallel()

	handler := NewSequentialSignalHandler()

	channelOne := make(chan *Signal)
	handler.(SignalRegistrar).AddSignal(channelOne)

	channelTwo := make(chan *Signal, 10)
	handler.(SignalRegistrar).AddSignal(channelTwo)

	writeSignals(handler, 1000)

	readSignals(t, channelTwo)
}

// Test that removing one channel results in no more messages being
// written to that channel.
func TestSequentialHandler_RemoveOneChannelOfOne(t *testing.T) {
	t.Parallel()
	handler := NewSequentialSignalHandler()

	channelOne := make(chan *Signal)
	handler.(SignalRegistrar).AddSignal(channelOne)

	writeSignals(handler, 1000)

	handler.(SignalRegistrar).RemoveSignal(channelOne)

	count, closed := countSignals(channelOne)
	if count > 1 {
		t.Error("handler continued writing to channel after removal")
	}
	if closed {
		t.Error("handler closed channel on .RemoveChannel()")
	}
}

// Test that removing one channel results in no more messages being
// written to that channel, and the other channels are unaffected.
func TestSequentialHandler_RemoveOneChannelOfMany(t *testing.T) {
	t.Parallel()
	handler := NewSequentialSignalHandler()

	channelOne := make(chan *Signal)
	handler.(SignalRegistrar).AddSignal(channelOne)

	channelTwo := make(chan *Signal, 10)
	handler.(SignalRegistrar).AddSignal(channelTwo)

	channelThree := make(chan *Signal, 2)
	handler.(SignalRegistrar).AddSignal(channelThree)

	writeSignals(handler, 1000)

	handler.(SignalRegistrar).RemoveSignal(channelTwo)
	defer close(channelTwo)

	count, closed := countSignals(channelTwo)
	if count > 10 {
		t.Error("handler continued writing to channel after removal")
	}
	if closed {
		t.Error("handler closed channel on .RemoveChannel()")
	}

	// Check that closing channel two does not close channel one.
	readSignals(t, channelOne)

	// Check that closing channel two does not close channel three.
	readSignals(t, channelThree)
}

// Test that Terminate() closes all channels that were attached at the time.
func TestSequentialHandler_TerminateClosesAllChannels(t *testing.T) {
	t.Parallel()
	handler := NewSequentialSignalHandler()

	channelOne := make(chan *Signal)
	handler.(SignalRegistrar).AddSignal(channelOne)

	channelTwo := make(chan *Signal, 10)
	handler.(SignalRegistrar).AddSignal(channelTwo)

	writeSignals(handler, 1000)

	handler.(Terminator).Terminate()

	count, closed := countSignals(channelOne)
	if count > 1 {
		t.Errorf("handler continued writing to channel after termination; read %v signals", count)
	}
	if !closed {
		t.Error("handler failed to close channel on .Terminate()")
	}

	count, closed = countSignals(channelTwo)
	if count > 10 {
		t.Errorf("handler continued writing to channel after termination; read %v signals", count)
	}
	if !closed {
		t.Error("handler failed to close channel on .Terminate()")
	}
}

// Verifies that after termination, the handler does not process any further signals.
func TestSequentialHandler_TerminateTerminates(t *testing.T) {
	t.Parallel()
	handler := NewSequentialSignalHandler()
	handler.(Terminator).Terminate()

	channelOne := make(chan *Signal)
	handler.(SignalRegistrar).AddSignal(channelOne)

	writeSignals(handler, 10)

	count, _ := countSignals(channelOne)
	if count > 0 {
		t.Errorf("handler continued operating after termination; read %v signals", count)
	}
}

// Verifies calling .Terminate() more than once is equivalent to calling it just once.
func TestSequentialHandler_TerminateIdemopotent(t *testing.T) {
	t.Parallel()
	handler := NewSequentialSignalHandler()
	handler.(Terminator).Terminate()
	handler.(Terminator).Terminate()

	channelOne := make(chan *Signal)
	handler.(SignalRegistrar).AddSignal(channelOne)
	writeSignals(handler, 10)

	count, _ := countSignals(channelOne)
	if count > 0 {
		t.Errorf("handler continued operating after termination; read %v signals", count)
	}
}

// Verifies calling RemoveSignal after Terminate() does not cause any unusual
// behaviour (panics, etc.).
func TestSequentialHandler_RemoveAfterTerminate(t *testing.T) {
	t.Parallel()
	handler := NewSequentialSignalHandler()
	handler.(Terminator).Terminate()
	handler.(Terminator).Terminate()

	channelOne := make(chan *Signal)
	handler.(SignalRegistrar).AddSignal(channelOne)
	handler.(SignalRegistrar).RemoveSignal(channelOne)
	writeSignals(handler, 10)

	count, _ := countSignals(channelOne)
	if count > 0 {
		t.Errorf("handler continued operating after termination; read %v signals", count)
	}
}

func writeSignals(handler SignalHandler, count int) {
	for i := 1; i <= count; i++ {
		signal := &Signal{Sequence: Sequence(i)}
		handler.DeliverSignal("iface", "name", signal)
	}
}

func readSignals(t *testing.T, channel <-chan *Signal) {
	// Overly generous timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	for i := 1; i <= 1000; i++ {
		select {
		case signal := <-channel:
			if signal.Sequence != Sequence(i) {
				t.Errorf("Received signal out of order. Expected %v, got %v", i, signal.Sequence)
				return
			}
		case <-ctx.Done():
			t.Error("Timeout occurred before all messages received")
			return
		}
	}
}

func countSignals(channel <-chan *Signal) (count int, closed bool) {
	count = 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()
	for {
		select {
		case _, ok := <-channel:
			if ok {
				count++
			} else {
				// Channel closed
				return count, true
			}
		case <-ctx.Done():
			return count, false
		}
	}
}
