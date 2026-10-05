package jobs

import (
	"context"
	"regexp"
	"sync"
	"time"
)

// watchCarry is how much of the previous write each pattern keeps. A match
// split across two writes is still seen, and a stream that never sends a
// newline cannot grow this without end.
const watchCarry = 4 << 10

// WaitOptions is what one wait is willing to be woken by. The zero value waits
// for the jobs to finish and nothing else.
type WaitOptions struct {
	// Timeout returns what is known so far instead of blocking further.
	Timeout time.Duration
	// Match returns as soon as a job writes something it matches. The caller
	// supplies it: only the caller knows what it waits for, and a host picking
	// which words deserve an interruption is the guess this avoids.
	Match *regexp.Regexp
}

// WaitOutcome says why a wait ended. A job still running means one thing after
// a match and another after a timeout, and the caller cannot tell from status.
type WaitOutcome string

const (
	WaitFinished  WaitOutcome = "finished"
	WaitMatched   WaitOutcome = "matched"
	WaitTimedOut  WaitOutcome = "timeout"
	WaitCancelled WaitOutcome = "cancelled"
)

// outputWatch wakes one wait when a job writes something its pattern matches.
type outputWatch struct {
	re    *regexp.Regexp
	hit   chan struct{}
	fired bool
	carry []byte
}

// notifyWatches is called under the job's lock for every write.
func (j *Job) notifyWatches(p []byte) {
	for _, w := range j.watches {
		if w.fired {
			continue
		}
		window := append(w.carry, p...)
		if w.re.Match(window) {
			w.fired = true
			close(w.hit)
			continue
		}
		w.carry = lastBytes(window, watchCarry)
	}
}

func (j *Job) watch(re *regexp.Regexp) *outputWatch {
	w := &outputWatch{re: re, hit: make(chan struct{})}
	j.mu.Lock()
	defer j.mu.Unlock()
	// What already arrived counts, or a caller that asked a moment too late
	// waits out its whole timeout for something already true. The retained
	// tail is the window such a caller can expect an answer from.
	if re.Match(j.tail) {
		w.fired = true
		close(w.hit)
		return w
	}
	// Seeded from what is already there. A watch registered mid-line would
	// otherwise never match a pattern the next write only completes — the
	// half it needed arrived before it was watching.
	w.carry = lastBytes(j.tail, watchCarry)
	j.watches = append(j.watches, w)
	return w
}

// lastBytes copies at most n trailing bytes, so a carry cannot alias a buffer
// that keeps being written to.
func lastBytes(b []byte, n int) []byte {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return append([]byte(nil), b...)
}

func (j *Job) unwatch(target *outputWatch) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i, w := range j.watches {
		if w == target {
			j.watches = append(j.watches[:i], j.watches[i+1:]...)
			return
		}
	}
}

func (j *Job) producedBytes() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.written
}

// WaitForSession blocks until the jobs finish, one of them writes something
// opts.Match matches, the timeout expires, or ctx ends. Each result carries
// what the job produced while this call was waiting, which is the only reading
// that tells a task making slow progress from one that has stopped.
func (m *Manager) WaitForSession(ctx context.Context, parentSession string, ids []string, opts WaitOptions) ([]Result, WaitOutcome) {
	targets := m.resolve(parentSession, ids)
	if len(targets) == 0 {
		return nil, WaitFinished
	}
	before := make(map[string]int64, len(targets))
	for _, j := range targets {
		before[j.ID] = j.producedBytes()
	}
	finish := func(outcome WaitOutcome) ([]Result, WaitOutcome) {
		out := m.results(targets)
		for i := range out {
			if out[i].Progress != nil {
				out[i].Progress.Delta = maxInt64(0, out[i].Progress.Produced-before[out[i].ID])
			}
		}
		return out, outcome
	}

	var timeout <-chan time.Time
	if opts.Timeout > 0 {
		t := time.NewTimer(opts.Timeout)
		defer t.Stop()
		timeout = t.C
	}

	var matched <-chan struct{}
	if opts.Match != nil {
		// One channel for all of them: the first job to match ends the wait,
		// which is what asking to be told when something happens means.
		merged, done := make(chan struct{}), make(chan struct{})
		defer close(done)
		var once sync.Once
		for _, j := range targets {
			w := j.watch(opts.Match)
			defer j.unwatch(w)
			go func(w *outputWatch) {
				select {
				case <-w.hit:
					once.Do(func() { close(merged) })
				case <-done:
				}
			}(w)
		}
		matched = merged
	}

	for _, j := range targets {
		select {
		case <-j.done:
		case <-ctx.Done():
			return finish(WaitCancelled)
		case <-timeout:
			return finish(WaitTimedOut)
		case <-matched:
			return finish(WaitMatched)
		}
	}
	return finish(WaitFinished)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
