package agent

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// ContentDigest returns the canonical digest used by the session WAL and
// revision ledger for the current in-memory transcript.
func (s *Session) ContentDigest() (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil session")
	}
	return ContentDigestForMessages(s.Snapshot())
}

// ContentDigestForMessages returns the canonical transcript digest for an
// immutable message snapshot. Frontends use it to bind a rendered history page
// to the exact content it contains instead of sampling a sidecar revision that
// may have advanced before or after the page was built.
func ContentDigestForMessages(msgs []provider.Message) (string, error) {
	digest, err := digestSessionMessages(msgs)
	if err != nil {
		return "", err
	}
	return digestString(digest), nil
}

// SessionsShareContent reports whether two saved sessions decode to the same
// transcript. It replaces byte-comparing the .jsonl checkpoints, which stopped
// implying transcript equality once the event log became authoritative: two
// identical checkpoints can hide diverged event logs.
func SessionsShareContent(pathA, pathB string) (bool, error) {
	msgsA, _, _, err := loadSessionMessages(pathA)
	if err != nil {
		return false, err
	}
	msgsB, _, _, err := loadSessionMessages(pathB)
	if err != nil {
		return false, err
	}
	digestA, err := digestSessionMessages(msgsA)
	if err != nil {
		return false, err
	}
	digestB, err := digestSessionMessages(msgsB)
	if err != nil {
		return false, err
	}
	return bytes.Equal(digestA[:], digestB[:]), nil
}

// SessionUserMessage is one user-role message with the best-known wall-clock
// time. Messages restored from a replace event (compaction, rewind) lose their
// per-turn times and report zero; callers apply their own fallback.
type SessionUserMessage struct {
	Text string
	At   time.Time
}

// LoadSessionUserMessages returns the session's user-role messages in
// transcript order, event-log aware. Direct .jsonl decoding misses everything
// after the first save once an event log exists, so surfaces like prompt
// history must use this instead.
func LoadSessionUserMessages(path string) ([]SessionUserMessage, error) {
	return loadSessionUserMessagesWithLimits(path, defaultSessionReplayLimits)
}

func loadSessionUserMessagesWithLimits(path string, limits sessionReplayLimits) ([]SessionUserMessage, error) {
	probe, err := probeSessionEventLogWithLimits(path, limits)
	if err != nil {
		return nil, err
	}
	if probe.futureSchema {
		return nil, fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", path, probe.schemaVersion, sessionEventSchemaVersion)
	}
	if probe.native && probe.size > 0 {
		replay, err := replaySessionEventLogWithLimits(store.SessionEventLog(path), limits)
		if err != nil {
			return nil, err
		}
		if replay.records > 0 {
			out := make([]SessionUserMessage, 0, len(replay.msgs))
			for i, m := range replay.msgs {
				if m.Role != provider.RoleUser {
					continue
				}
				at := time.Time{}
				if i < len(replay.times) {
					at = replay.times[i]
				}
				if m.CreatedAt > 0 {
					at = time.UnixMilli(m.CreatedAt)
				}
				out = append(out, SessionUserMessage{Text: m.Content, At: at})
			}
			return out, nil
		}
	}
	msgs, err := loadSessionMessagesFromJSONL(path)
	if err != nil {
		return nil, err
	}
	out := make([]SessionUserMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != provider.RoleUser {
			continue
		}
		at := time.Time{}
		if m.CreatedAt > 0 {
			at = time.UnixMilli(m.CreatedAt)
		}
		out = append(out, SessionUserMessage{Text: m.Content, At: at})
	}
	return out, nil
}

// SessionContentModTime returns when the session transcript last changed on
// disk: the newer of the .jsonl checkpoint and the event log. The checkpoint
// alone goes stale between checkpoints, so recency ordering must use this.
func SessionContentModTime(path string) time.Time {
	var mod time.Time
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		mod = info.ModTime()
	}
	if logPath := store.SessionEventLog(path); logPath != "" {
		if info, err := os.Stat(logPath); err == nil && !info.IsDir() && info.ModTime().After(mod) {
			mod = info.ModTime()
		}
	}
	return mod
}
