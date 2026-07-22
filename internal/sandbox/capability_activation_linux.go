//go:build linux

package sandbox

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

const (
	capabilityActivationInterruptedWait = 25 * time.Millisecond
	capabilityActivationCloseWait       = 100 * time.Millisecond
)

// capabilityActivationWitness consumes Bubblewrap's json-status stream. Its
// final exit-code event is emitted only after sandbox setup completed and the
// requested executable was successfully exec'd; the earlier child-pid event is
// deliberately not treated as activation proof.
type capabilityActivationWitness struct {
	reader *os.File
	writer *os.File

	applied     chan struct{}
	done        chan struct{}
	appliedOnce sync.Once
	writerOnce  sync.Once
	closeOnce   sync.Once
}

func newCapabilityActivationWitness() (*capabilityActivationWitness, *os.File, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	witness := &capabilityActivationWitness{
		reader:  reader,
		writer:  writer,
		applied: make(chan struct{}),
		done:    make(chan struct{}),
	}
	go witness.drain()
	return witness, writer, nil
}

func (w *capabilityActivationWitness) drain() {
	defer close(w.done)
	decoder := json.NewDecoder(w.reader)
	for {
		var event struct {
			ExitCode *int `json:"exit-code"`
		}
		if err := decoder.Decode(&event); err != nil {
			return
		}
		if event.ExitCode != nil {
			w.appliedOnce.Do(func() { close(w.applied) })
		}
	}
}

func (w *capabilityActivationWitness) state(outcome CapabilityExecutionOutcome) CapabilityAppliedState {
	if w == nil {
		return CapabilityApplicationUnknown
	}
	w.closeWriter()
	if channelClosed(w.applied) {
		return CapabilityApplied
	}
	wait, bounded := capabilityActivationWait(outcome)
	if !bounded {
		// exec.Cmd.Wait has completed, so Bubblewrap has closed its inherited
		// writer. Closing our copy makes the decoder's result deterministic:
		// either it observes the final event or it reaches EOF without one.
		select {
		case <-w.applied:
			return CapabilityApplied
		case <-w.done:
			if channelClosed(w.applied) {
				return CapabilityApplied
			}
			return CapabilityNotApplied
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-w.applied:
		return CapabilityApplied
	case <-w.done:
		if channelClosed(w.applied) {
			return CapabilityApplied
		}
		return CapabilityApplicationUnknown
	case <-timer.C:
		// Prefer an event decoded concurrently with the deadline.
		if channelClosed(w.applied) {
			return CapabilityApplied
		}
		return CapabilityApplicationUnknown
	}
}

func capabilityActivationWait(outcome CapabilityExecutionOutcome) (time.Duration, bool) {
	if outcome == CapabilityExecutionCompleted {
		return 0, false
	}
	return capabilityActivationInterruptedWait, true
}

func (w *capabilityActivationWitness) close() {
	if w == nil {
		return
	}
	w.closeOnce.Do(func() {
		w.closeWriter()
		_ = w.reader.Close()
		timer := time.NewTimer(capabilityActivationCloseWait)
		defer timer.Stop()
		select {
		case <-w.done:
		case <-timer.C:
		}
	})
}

func (w *capabilityActivationWitness) closeWriter() {
	w.writerOnce.Do(func() { _ = w.writer.Close() })
}

func channelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}
