package workspacelease

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"time"

	"reasonix/internal/base/fileutil"
)

// claimLogKeep bounds the record of who claimed what. A yield older than this
// many claims reports the contention it can still prove, never one it guessed.
const claimLogKeep = 64

// claimEntry is one granted claim, appended while the guard is held so Seq is
// unique and ordered across processes. Reset marks the point where an
// unreadable log was started over: what came before it is unknown.
type claimEntry struct {
	Seq       uint64   `json:"seq"`
	SessionID string   `json:"sessionId,omitempty"`
	Holder    string   `json:"holder,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Reset     bool     `json:"reset,omitempty"`
}

// Contender is another session that was granted a claim overlapping the paths
// this one gave up while it yielded. It comes from the claim record the lease
// itself keeps, not from anything either session said.
type Contender struct {
	Holder    string
	SessionID string
	// Paths is the extent that session claimed; empty means the whole workspace.
	Paths []string
}

// Contention is what a yield can say about the ground it gave up. Unknown means
// the claim record could not answer, which is not the same as nobody claiming.
type Contention struct {
	Contenders []Contender
	Unknown    bool
}

// Any reports whether the resume has something to tell.
func (c Contention) Any() bool { return len(c.Contenders) > 0 || c.Unknown }

// claimMark is the claim record as a yield found it.
type claimMark struct {
	seq      uint64
	readable bool
	failures uint64
}

func (o *Owner) claimLogPath() string { return o.lockPath + ".log" }

// readClaimLog reports ok=false when the log exists and cannot be read as one.
func readClaimLog(path string) (entries []claimEntry, ok bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, true
	}
	if err != nil || json.Unmarshal(data, &entries) != nil {
		return nil, false
	}
	return entries, true
}

// appendClaim records a granted claim. The caller holds the guard lock. The log
// only explains contention afterwards, so a failure here never refuses a claim.
func (o *Owner) appendClaim(paths []string) error {
	entries, ok := readClaimLog(o.claimLogPath())
	seq := uint64(time.Now().UnixNano())
	if n := len(entries); n > 0 {
		seq = max(seq, entries[n-1].Seq+1)
	}
	if !ok {
		entries = []claimEntry{{Seq: seq, Reset: true}}
		seq++
	}
	entries = append(entries, claimEntry{Seq: seq, SessionID: o.sessionID(), Holder: o.holderName(), Paths: paths})
	if len(entries) > claimLogKeep {
		entries = entries[len(entries)-claimLogKeep:]
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(o.claimLogPath(), data, 0o600)
}

// claimMark reads the claim record at the moment a session gives its claim up.
// It runs outside the guard, which is safe only because this session still
// holds its claim: nothing overlapping can be granted until it is released.
func (o *Owner) claimMark() claimMark {
	entries, ok := readClaimLog(o.claimLogPath())
	m := claimMark{readable: ok, failures: o.local.logFailures.Load()}
	if n := len(entries); n > 0 {
		m.seq = entries[n-1].Seq
	}
	return m
}

// contendersSince names every other session granted a claim overlapping paths
// after the mark, or says the record cannot tell.
func (o *Owner) contendersSince(mark claimMark, paths []string) Contention {
	entries, ok := readClaimLog(o.claimLogPath())
	out := Contention{Unknown: !ok || !mark.readable || o.local.logFailures.Load() != mark.failures}
	if len(entries) == claimLogKeep && entries[0].Seq > mark.seq {
		out.Unknown = true
	}
	self := o.sessionID()
	for _, e := range entries {
		if e.Seq <= mark.seq {
			continue
		}
		if e.Reset {
			out.Unknown = true
			continue
		}
		if e.SessionID == self || !pathsOverlap(paths, e.Paths) {
			continue
		}
		i := slices.IndexFunc(out.Contenders, func(c Contender) bool { return c.SessionID == e.SessionID })
		if i < 0 {
			out.Contenders = append(out.Contenders, Contender{Holder: e.Holder, SessionID: e.SessionID, Paths: slices.Clone(e.Paths)})
			continue
		}
		if len(e.Paths) == 0 || len(out.Contenders[i].Paths) == 0 {
			out.Contenders[i].Paths = nil
		} else {
			out.Contenders[i].Paths = append(out.Contenders[i].Paths, e.Paths...)
		}
	}
	return out
}

// writeClaim records a granted claim in its own file and, best effort, in the
// claim log. A log that could not be written is counted for this process; a
// session in another process has no way to learn of it.
func (o *Owner) writeClaim(path string, paths []string) error {
	data, err := json.Marshal(pathLeaseRecord{Holder: o.holderName(), SessionID: o.sessionID(), Paths: paths})
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return err
	}
	if o.appendClaim(paths) != nil {
		o.local.logFailures.Add(1)
	}
	return nil
}
