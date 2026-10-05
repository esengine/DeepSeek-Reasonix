package proc

import (
	"errors"
	"os/exec"
	"sync"
)

// ErrProcessTrackingUnavailable means a caller that requires an OS-enforced
// process-tree lifetime could not establish it before the child began running.
var ErrProcessTrackingUnavailable = errors.New("process tree tracking is unavailable")

// TrackedJob owns the OS handle StartTracked established, so releasing it is
// the handle's business rather than something several teardown paths agree
// about. Closing a Windows Job Object twice frees a value the OS may already
// have reissued, and the second close lands on whatever now owns it.
type TrackedJob struct {
	once sync.Once
	h    uintptr
	// What the one-shot performs. Tests count releases through it: off Windows
	// the real one is a no-op, so watching the handle would prove nothing here.
	release func()
}

func (j *TrackedJob) releaseOnce() {
	j.once.Do(func() {
		if j.release != nil {
			j.release()
			return
		}
		finishTracked(j.h)
	})
}

// Kill terminates the tree and releases the handle, exactly once. A later call
// still kills the direct child, because a caller asking twice is asking about
// the process, not about the handle.
func (j *TrackedJob) Kill(cmd *exec.Cmd) {
	if j == nil {
		KillTree(cmd)
		return
	}
	released := false
	j.once.Do(func() {
		if j.release != nil {
			j.release()
		} else {
			killTracked(cmd, j.h)
		}
		released = true
	})
	if !released && cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// Tracked says whether an OS handle backs this job. Off Windows the process
// group does the work and there is none, so a caller that only cares about the
// handle can tell the two apart.
func (j *TrackedJob) Tracked() bool { return j != nil && j.h != 0 }

// Finish releases the handle once the process is known to be gone.
func (j *TrackedJob) Finish() {
	if j == nil {
		return
	}
	j.releaseOnce()
}
