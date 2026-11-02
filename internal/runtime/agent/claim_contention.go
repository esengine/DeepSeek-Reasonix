// claim_contention.go — what the host tells the model, and the receipt, when a
// session gave up its write claim to wait on a person and another session was
// granted that ground meanwhile.
package agent

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/runtime/completion"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/workspacelease"
)

// CodeClaimContended identifies the host notice a model can cite.
const CodeClaimContended = "workspace.claim_contended"

const (
	maxContendersNamed    = 3
	maxContenderPaths     = 4
	maxContenderIDRunes   = 64
	maxContenderPathRunes = 200
)

// claimYieldLog holds the contended yields not yet delivered on a receipt. It
// outlives a turn only until the next receipt reads it.
type claimYieldLog struct {
	mu      sync.Mutex
	pending []string
}

// NoteClaimContended queues the typed notice for the next round and remembers
// the contention for the receipt of the turn it happened in. It says nothing
// for a yield nobody claimed through. Only what the lease recorded reaches the
// model, and a session's own wording never does: the id is the one thing named.
func (a *Agent) NoteClaimContended(c workspacelease.Contention) {
	if a == nil || !c.Any() {
		return
	}
	a.NoteHostFact(CodeClaimContended + ": " + claimContendedText(c))
	a.sess.claimYields.mu.Lock()
	detail := claimContenderNames(c)
	a.sess.claimYields.pending = append(a.sess.claimYields.pending, detail)
	a.sess.claimYields.mu.Unlock()
	a.svc.mutationObserver.RecordGap(checkpoint.CoverageGap{
		Reason: checkpoint.GapWorkspaceClaimYielded,
		Detail: detail,
	})
}

// quoted renders text another session wrote as one bounded, escaped token.
func quoted(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit]) + "…"
	}
	return strconv.Quote(s)
}

func claimContenderNames(c workspacelease.Contention) string {
	names := make([]string, 0, len(c.Contenders)+1)
	for _, x := range c.Contenders {
		names = append(names, contenderLabel(x))
	}
	if c.Unknown {
		names = append(names, "claim record unreadable")
	}
	return strings.Join(names, ", ")
}

func contenderLabel(x workspacelease.Contender) string {
	if x.SessionID == "" {
		return "another session"
	}
	return "session " + quoted(x.SessionID, maxContenderIDRunes)
}

func claimContendedText(c workspacelease.Contention) string {
	var b strings.Builder
	b.WriteString("while this session waited on the user it gave up its write claim")
	if len(c.Contenders) > 0 {
		b.WriteString(", and another session was granted an overlapping one: ")
	}
	for i, x := range c.Contenders {
		if i == maxContendersNamed {
			fmt.Fprintf(&b, "; and %d more", len(c.Contenders)-i)
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(contenderLabel(x))
		b.WriteString(" over ")
		switch {
		case len(x.Paths) == 0:
			b.WriteString("the whole workspace")
		default:
			shown := slices.Clone(x.Paths[:min(len(x.Paths), maxContenderPaths)])
			for j, p := range shown {
				shown[j] = quoted(p, maxContenderPathRunes)
			}
			b.WriteString(strings.Join(shown, ", "))
			if len(x.Paths) > maxContenderPaths {
				fmt.Fprintf(&b, " and %d more", len(x.Paths)-maxContenderPaths)
			}
		}
	}
	if c.Unknown {
		b.WriteString(". The lease's claim record could not be read in full, so whether another session wrote there is not known")
	}
	b.WriteString(". Files there may have changed since this session last read or wrote them; read them again before editing or relying on an earlier read.")
	return b.String()
}

// takeClaimYieldGaps hands the receipt every contended yield since the last one.
func (a *Agent) takeClaimYieldGaps() []completion.Gap {
	a.sess.claimYields.mu.Lock()
	pending := a.sess.claimYields.pending
	a.sess.claimYields.pending = nil
	a.sess.claimYields.mu.Unlock()
	gaps := make([]completion.Gap, 0, len(pending))
	for _, names := range pending {
		gaps = append(gaps, completion.Gap{Kind: completion.GapWorkspaceClaimYielded, Detail: names})
	}
	return gaps
}
