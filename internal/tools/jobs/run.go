package jobs

import (
	"context"
	"io"

	"reasonix/internal/contract/event"
)

// launchJob runs j in its own goroutine. Joining the wait group and seeing the
// root cancelled happen under m.mu, the lock Close cancels under, so a job
// either joins before Close waits or is interrupted without ever running.
func (m *Manager) launchJob(ctx context.Context, j *Job, run func(context.Context, io.Writer) (string, error)) {
	parentSession, id, kind, label := j.SessionID, j.ID, j.Kind, j.Label
	m.mu.Lock()
	if err := m.root.Err(); err != nil {
		m.mu.Unlock()
		j.mu.Lock()
		j.times.finished = nowMs()
		j.artifact.close()
		if j.remote != nil {
			j.remote.finishErr = err
			if j.remote.release != nil {
				j.remote.release()
			}
		}
		if metaErr := m.writeJobMetaLocked(j, Interrupted); metaErr != nil {
			j.noteArtifactErr(metaErr.Error())
		}
		resultRef := j.artifactRefLocked()
		remote := j.remote != nil
		j.mu.Unlock()
		// A remote task refused here may still run remotely; its checkpoint, not
		// a completion, is what the next owner resumes from.
		var ev CompletionEvent
		if !remote {
			ev = m.recordCompletion(parentSession, id, kind, label, resultRef, Interrupted, err)
		}
		j.mu.Lock()
		j.status = Interrupted
		j.mu.Unlock()
		close(j.done)
		m.publishCompletion(ev)
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
		result, err := runRecovered(ctx, jobWriter{j}, run)
		j.mu.Lock()
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
		pendingRemote := j.remote != nil && (!j.remote.settled || st == Interrupted)
		j.mu.Unlock()
		// An observable terminal status must imply its completion is already
		// queued: Wait only blocks on Running jobs, so recording second would let
		// a drain race ahead of the bookkeeping.
		var ev CompletionEvent
		if !pendingRemote {
			ev = m.remoteCompletion(j, resultRef, st, err)
		} else if m.root.Err() == nil && err != nil {
			m.emitIfActive(parentSession, event.Event{Kind: event.Notice, Level: event.LevelWarn,
				Text: "Remote task stopped locally; its remote state remains unknown. Resume the session to reconcile the original task.", Detail: id + ": " + err.Error()})
		}

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
		m.publishRemoteCompletion(j)
	}()
}
