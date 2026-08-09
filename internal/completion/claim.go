package completion

import (
	"encoding/json"
	"strings"

	"reasonix/internal/evidence"
)

// Claim is the model's own account of the work, as passed to update_goal. It
// is the only model-authored part of a report, and it can only ever add to
// what the host found: Verified is checked against the ledger, while
// Unverified and Risks are declarations the host cannot verify but has no
// reason to suppress.
type Claim struct {
	Verified   []string
	Unverified []string
	Risks      []string
}

// Empty reports whether the turn made no claim at all.
func (c Claim) Empty() bool {
	return len(c.Verified) == 0 && len(c.Unverified) == 0 && len(c.Risks) == 0
}

// claimOf extracts the latest successful update_goal completion claim. A
// failed call claims nothing: outside an active goal turn the tool fails
// closed, and a rejected claim must not reach the report.
func claimOf(receipts []evidence.Receipt) Claim {
	var out Claim
	for _, r := range receipts {
		if !r.Success || r.ToolName != "update_goal" || len(r.Args) == 0 {
			continue
		}
		var payload struct {
			Completion struct {
				Verified   []string `json:"verified"`
				Unverified []string `json:"unverified"`
				Risks      []string `json:"risks"`
			} `json:"completion"`
		}
		if json.Unmarshal(r.Args, &payload) != nil {
			continue
		}
		out = Claim{
			Verified:   trimAll(payload.Completion.Verified),
			Unverified: trimAll(payload.Completion.Unverified),
			Risks:      trimAll(payload.Completion.Risks),
		}
	}
	return out
}

func trimAll(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// reconcile folds the claim into a host-built report. Claimed verifications
// are matched against real receipts, and every mismatch becomes a gap; the
// model's own declarations are carried through untouched. Nothing here can
// clear a gap the host found — a claim only ever adds.
func reconcile(rep Report, claim Claim, receipts []evidence.Receipt) Report {
	rep.Claimed = claim
	rep.Risks = claim.Risks
	var gaps []Gap
	for _, command := range claim.Verified {
		if why := unbackedClaim(command, receipts); why != "" {
			gaps = append(gaps, Gap{GapUnbackedClaim, why})
		}
	}
	for _, note := range claim.Unverified {
		gaps = append(gaps, Gap{GapDeclaredUnverified, note})
	}
	rep.Gaps = append(gaps, rep.Gaps...)
	return rep
}

// unbackedClaim returns why a claimed verification is not backed by the
// ledger, or "" when a successful run of it survives the latest mutation.
func unbackedClaim(command string, receipts []evidence.Receipt) string {
	lastMutation := -1
	for i, r := range receipts {
		if r.Success && (r.Mutation || r.Write) {
			lastMutation = i
		}
	}
	matched, index, success := false, -1, false
	for i, r := range receipts {
		ran := strings.TrimSpace(r.Command)
		if ran == "" {
			continue
		}
		if ran != command && !evidence.CommandMatches(command, ran) {
			continue
		}
		matched, index, success = true, i, r.Success
	}
	switch {
	case !matched:
		return command + " — claimed as verification, but no run of it was recorded"
	case !success:
		return command + " — claimed as verification, but its last run failed"
	case index < lastMutation:
		return command + " — claimed as verification, but it last ran before the latest change"
	default:
		return ""
	}
}
