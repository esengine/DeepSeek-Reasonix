package jobs

import "time"

// Progress is what is known about a job that has not finished. A task that
// redirects its output into a file is silent on its own streams while working
// hard, and the caller cannot tell that from a hang without both numbers.
type Progress struct {
	// Running is how long the job has been going.
	Running time.Duration
	// Silent is how long since it last wrote to its own streams.
	Silent time.Duration
	// Spoke is false for a job that never wrote to them at all — the shape of
	// one whose output went somewhere else entirely.
	Spoke bool
	// Produced is everything written so far; Delta is how much arrived while
	// the caller waited. Readings, not thresholds — slow progress and a stop
	// differ here, and which one matters is the caller's to know.
	Produced int64
	Delta    int64
}

func (m *Manager) results(targets []*Job) []Result {
	out := make([]Result, 0, len(targets))
	for _, j := range targets {
		j.mu.Lock()
		text := j.result
		if text == "" && j.artifact.path != "" {
			text = j.readArtifactAllLocked()
		}
		if text == "" {
			text = string(j.tail)
		}
		if j.artifact.err != "" {
			if text != "" {
				text += "\n"
			}
			text += "job artifact incomplete: " + j.artifact.err
		}
		r := Result{ID: j.ID, Kind: j.Kind, Label: j.Label, Status: j.status, Output: text}
		if j.status == Running {
			r.Progress = &Progress{
				Running:  sinceMs(j.times.started),
				Silent:   sinceMs(j.times.activity),
				Spoke:    j.written > 0,
				Produced: j.written,
			}
		}
		out = append(out, r)
		j.mu.Unlock()
	}
	return out
}

// sinceMs is how long ago a millisecond stamp was, or zero if it never was.
func sinceMs(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Since(time.UnixMilli(ms))
}
