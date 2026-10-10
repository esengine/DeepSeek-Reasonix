package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/runtime/verdict"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/observation"
	"reasonix/internal/state/trustedstate"
)

// EvidenceSeal is where the root agent seals each turn's shadow evidence
// bundle: one Trusted Host State stream per workspace root.
type EvidenceSeal struct {
	Store  *trustedstate.Store
	Stream string
	// Root and Observer snapshot the workspace around each turn; either unset
	// leaves the bundle without workspace observation.
	Root     string
	Observer *observation.Observer
}

// shadowBundleKind names the record kind. The bundle observes and gates
// nothing: nothing reads it back to decide a turn.
const shadowBundleKind = "shadow_bundle/1"

// sealWait bounds how long a turn's end waits on another process holding the
// stream's lock. Past it the turn ends unsealed and says why.
const sealWait = 2 * time.Second

type shadowBundle struct {
	Kind         string                     `json:"kind"`
	InputDigest  string                     `json:"input_digest"`
	Contract     shadowContract             `json:"contract"`
	Report       completion.Report          `json:"report"`
	Receipts     []sealedReceipt            `json:"receipts"`
	Blocked      bool                       `json:"blocked,omitempty"`
	Criteria     []taskcontract.Requirement `json:"criteria,omitempty"`
	TaskContract contractState              `json:"task_contract"`
	Verdict      bundleVerdict              `json:"verdict"`
	Divergence   verdict.Divergence         `json:"divergence"`
	workspaceObservation
}

// bundleVerdict keeps the outcome inline and the obligations as an object of
// their own. The list tracks the contract's size, and an object repeats nothing
// when a turn leaves every obligation as it was.
type bundleVerdict struct {
	Outcome     verdict.Outcome     `json:"outcome"`
	Obligations trustedstate.Digest `json:"obligations,omitempty"`
	Count       int                 `json:"obligation_count"`
}

// sealVerdict files the obligations in the store and returns the bundle's view.
func sealVerdict(store *trustedstate.Store, res verdict.Result) (bundleVerdict, error) {
	out := bundleVerdict{Outcome: res.Outcome, Count: len(res.Obligations)}
	if len(res.Obligations) == 0 {
		return out, nil
	}
	body, err := json.Marshal(res.Obligations)
	if err != nil {
		return out, err
	}
	out.Obligations, err = store.PutObject(body)
	return out, err
}

type shadowContract struct {
	Kind   string               `json:"kind"`
	Risk   uint8                `json:"risk"`
	Checks []taskcontract.Check `json:"checks,omitempty"`
}

// sealedReceipt keeps a receipt's facts and only the digest of its arguments,
// which can carry whole file bodies.
type sealedReceipt struct {
	evidence.Receipt
	ArgsDigest string `json:"args_digest,omitempty"`
}

func sealReceipts(receipts []evidence.Receipt) []sealedReceipt {
	out := make([]sealedReceipt, 0, len(receipts))
	for _, r := range receipts {
		s := sealedReceipt{Receipt: r}
		if len(r.Args) > 0 {
			s.ArgsDigest = sha256Hex(r.Args)
			s.Args = nil
		}
		out = append(out, s)
	}
	return out
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sealShadowBundle writes the turn's contract, report and receipts as the next
// record of the workspace stream and reports the seal. A failed seal changes
// nothing about the turn: the bundle is a shadow, and the audit carries the
// store's typed cause instead.
func (a *Agent) sealShadowBundle(input string, c *taskcontract.Contract, rep completion.Report, receipts []evidence.Receipt, blocked bool) {
	seal := a.svc.evidenceSeal
	if seal == nil || seal.Store == nil || seal.Stream == "" {
		return
	}
	obs := a.observeWorkspace(seal)
	ctx, cancel := context.WithTimeout(context.Background(), sealWait)
	defer cancel()
	cs := a.settleContract(ctx, seal)
	res := verdict.Evaluate(verdict.Input{
		Frozen:          a.frozenResults(cs.Criteria),
		CoveredCriteria: a.planCriteriaCovered(cs.Criteria),
		Contract:        c,
		Receipts:        receipts,
		HostObligations: a.task.ledger.Obligations(a.checkContract()),
		Blocked:         c.Blocked(),
	})
	div := verdict.Classify(rep.Verdict, c, res)
	if cs.Failure == "" {
		a.turn.outcome = &res
	}
	sealed, putErr := sealVerdict(seal.Store, res)
	payload, err := json.Marshal(shadowBundle{
		Kind:         shadowBundleKind,
		InputDigest:  sha256Hex([]byte(input)),
		Contract:     shadowContract{Kind: c.Kind.String(), Risk: uint8(c.Risk), Checks: c.Checks},
		Report:       rep,
		Receipts:     sealReceipts(receipts),
		Blocked:      blocked,
		Criteria:     c.Requirements,
		TaskContract: cs,
		Verdict:      sealed,
		Divergence:   div,

		workspaceObservation: obs,
	})
	audit := event.EvidenceBundleAudit{
		Receipts:           len(receipts),
		SnapshotComplete:   obs.After != nil && obs.After.Complete,
		UnobservedCompared: obs.Unobserved.Compared,
		UnobservedChanges:  obs.Unobserved.Changed,
		Outcome:            string(res.Outcome),
		DivergenceClass:    div.Class,
		DivergenceReasons:  div.Reasons,
		ContractRevision:   cs.Revision,
		ContractDecision:   string(cs.Decision),
		ContractFailure:    cs.Failure,
	}
	if putErr != nil {
		audit.FailureCode = trustedstate.FailureCode(putErr)
		event.RecordEvidenceBundle(a.svc.sink, audit)
		return
	}
	if err != nil {
		audit.FailureCode = "trusted_state.encode"
		event.RecordEvidenceBundle(a.svc.sink, audit)
		return
	}
	rec, digest, err := seal.Store.Append(ctx, seal.Stream, shadowBundleKind, payload)
	if err != nil {
		audit.FailureCode = trustedstate.FailureCode(err)
		event.RecordEvidenceBundle(a.svc.sink, audit)
		return
	}
	audit.Sealed = true
	audit.Record = string(digest)
	audit.Generation = rec.Generation
	audit.Integrity = string(rec.Integrity)
	event.RecordEvidenceBundle(a.svc.sink, audit)
}
