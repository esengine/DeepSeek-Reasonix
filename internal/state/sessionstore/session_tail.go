package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// SessionTail is the newest part of a history too large to replay whole.
// Messages starts with the system prompt when the history had one; Dropped is
// how many messages after it were left out.
type SessionTail struct {
	Messages []provider.Message
	Dropped  int
}

// trimToTail drops the oldest messages after the system prompt until the rest
// fit tailBudget, then any that would leave the tail starting mid-turn.
func (r *sessionEventReplay) trimToTail(limits sessionReplayLimits) {
	if limits.tailBudget == 0 {
		return
	}
	if len(r.sizes) > len(r.msgs) {
		r.sizes = r.sizes[:0]
	}
	for _, m := range r.msgs[len(r.sizes):] {
		b, _ := json.Marshal(m)
		r.sizes = append(r.sizes, int64(len(b)))
	}
	var total int64
	for _, n := range r.sizes {
		total += n
	}
	start := 0
	if len(r.msgs) > 0 && r.msgs[0].Role == provider.RoleSystem {
		start = 1
	}
	cut := start
	for cut < len(r.msgs)-1 && total > limits.tailBudget {
		total -= r.sizes[cut]
		cut++
	}
	if cut > start {
		next := cut
		for next < len(r.msgs) && r.msgs[next].Role != provider.RoleUser {
			next++
		}
		if next < len(r.msgs) {
			cut = next
		} else {
			for cut > start && r.msgs[cut].Role == provider.RoleTool {
				cut--
			}
		}
		r.dropped += cut - start
		r.msgs = slices.Delete(r.msgs, start, cut)
		r.sizes = slices.Delete(r.sizes, start, cut)
		r.times = slices.Delete(r.times, start, cut)
	}
	total = 0
	for _, n := range r.sizes {
		total += n
	}
	r.payloadBytes = total
	r.collectionItems = 0
}

// loadSessionTailWithLimits reads the newest messages of a history within
// budget bytes, whatever its total size. Nothing on disk is changed.
func loadSessionTailWithLimits(sessionPath string, limits sessionReplayLimits, budget int64) (SessionTail, error) {
	limits.tailBudget = budget
	probe, err := probeSessionEventLogWithLimits(sessionPath, limits)
	if err != nil {
		return SessionTail{}, err
	}
	switch {
	case probe.futureSchema:
		return SessionTail{}, fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", sessionPath, probe.schemaVersion, sessionEventImageBlobsSchemaVersion)
	case probe.dag:
		return loadSessionDAGTail(sessionPath, limits)
	case probe.native && probe.size > 0:
		replay, err := replaySessionEventLogWithLimits(store.SessionEventLog(sessionPath), limits)
		if err != nil {
			return SessionTail{}, err
		}
		if replay.damaged {
			return SessionTail{}, errors.New("session event log is damaged; its tail cannot be read safely")
		}
		return SessionTail{Messages: replay.msgs, Dropped: replay.dropped}, nil
	}
	return SessionTail{}, os.ErrNotExist
}

// SessionTailBudgetBytes is how much of a history too large to open a recovery
// branch carries over.
const SessionTailBudgetBytes = int64(16 << 20)

// LoadSessionTail reads the newest SessionTailBudgetBytes of the history at
// sessionPath without changing any file.
func LoadSessionTail(sessionPath string) (SessionTail, error) {
	return loadSessionTailWithLimits(sessionPath, defaultSessionReplayLimits, SessionTailBudgetBytes)
}
