package jobs

import (
	"context"
	"errors"
	"io"

	"reasonix/internal/base/secrets"
)

// launchJob runs j in its own goroutine. Joining the wait group and seeing the
// root cancelled happen under m.mu, the lock Close cancels under, so a job
// either joins before Close waits or is interrupted without ever running.
func (m *Manager) launchJob(ctx context.Context, j *Job, run func(context.Context, io.Writer) (string, error)) {
	parentSession, id, kind, label := j.SessionID, j.ID, j.Kind, j.Label
	m.mu.Lock()
	if err := m.root.Err(); err != nil {
		m.mu.Unlock()
		m.finishUninvokedJob(j, Interrupted, err)
		return
	}
	m.wg.Add(1)
	if m.stalledWarning > 0 {
		m.wg.Add(1)
		go m.monitorStalled(parentSession, j)
	}
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		// The tail, the watchers and the log on disk all read what passes here.
		out := secrets.NewStream(jobWriter{j})
		result, err := runRecovered(ctx, out, run)
		_ = out.Flush()
		cleanupErr := j.finalize(Finalization{Invoked: true, Err: err})
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		j.mu.Lock()
		if cleanupErr != nil {
			j.noteArtifactErr(cleanupErr.Error())
		}
		j.runReturned = true
		j.mu.Unlock()

		var st Status
		switch {
		case ctx.Err() != nil:
			st = m.cancelledStatus()
		case err != nil:
			st = Failed
			if result == "" {
				result = err.Error()
			}
		default:
			st = Done
		}
		result = secrets.Masker().String(result)
		finishedAt := nowMs()
		if result != "" {
			j.mu.Lock()
			if j.artifact.open() {
				j.artifact.writeString(result)
			} else {
				j.result = result
			}
			j.tail = appendTail(j.tail, []byte(result), defaultTailBytes)
			j.mu.Unlock()
		}
		targetDir := m.artifactTargetDirForJob(j)
		j.mu.Lock()
		j.artifact.close()
		if j.artifact.err != "" {
			j.artifact.complete = false
		}
		j.times.finished = finishedAt
		if targetDir != "" {
			if moveErr := j.moveArtifactToDirLocked(targetDir); moveErr != nil {
				j.noteArtifactErr("migration: " + moveErr.Error())
			}
		}
		metaErr := m.writeJobMetaLocked(j, st)
		if metaErr != nil {
			j.noteArtifactErr("metadata: " + metaErr.Error())
		}
		resultRef := j.artifactRefLocked()
		j.mu.Unlock()
		// An observable terminal status must imply its completion is already
		// queued: Wait only blocks on Running jobs, so recording second would let
		// a drain race ahead of the bookkeeping.
		ev := m.recordCompletion(parentSession, id, kind, label, resultRef, st, err)

		j.mu.Lock()
		if j.status != Killed && j.status != Interrupted { // a status published on cancel stands
			j.status = st
		}
		if j.artifact.path != "" && j.artifact.complete {
			j.result = ""
			j.tail = nil
		}
		j.mu.Unlock()
		close(j.done)
		// Only now would a woken turn's first question get "terminal".
		m.publishCompletion(ev)
	}()
}
