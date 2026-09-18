package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/agent"
	"reasonix/internal/attachment"
	"reasonix/internal/event"
	"reasonix/internal/session"
)

// SubmissionRequest fingerprints the actual operation, never only its label.
type SubmissionRequest struct {
	ID                string                 `json:"-"`
	HTTP              bool                   `json:"http,omitempty"`
	Input             string                 `json:"input"`
	Display           string                 `json:"display,omitempty"`
	Format            string                 `json:"format,omitempty"`
	Action            string                 `json:"action,omitempty"`
	RecoveryID        string                 `json:"recoveryId,omitempty"`
	Original          string                 `json:"original,omitempty"`
	Goal              string                 `json:"goal,omitempty"`
	ToolApprovalMode  string                 `json:"toolApprovalMode,omitempty"`
	Invocations       []InvocationRequest    `json:"invocations,omitempty"`
	DraftIDs          []string               `json:"draftIds,omitempty"`
	AttachmentDigests []string               `json:"attachmentDigests,omitempty"`
	Attachments       []SubmissionAttachment `json:"attachments,omitempty"`
	frozenSources     map[string]*attachment.AttachmentRef
	inheritedSources  []attachment.Source
}

// SubmissionAttachment describes an ordered logical item, independently of
// its temporary credential. Reference reads still require session authority.
type SubmissionAttachment struct {
	ClientAttachmentID string                    `json:"clientAttachmentId"`
	DraftID            string                    `json:"draftId,omitempty"`
	Path               string                    `json:"path,omitempty"`
	Reference          *attachment.AttachmentRef `json:"reference,omitempty"`
}

// ErrSubmissionNotAccepted is only attached to failures that precede durable
// admission. Transport cancellation and flush failures intentionally omit it:
// the caller must query the receipt before deciding whether a retry may run.
var ErrSubmissionNotAccepted = errors.New("submission not accepted")

type submissionIdentityState struct {
	mu        sync.Mutex
	pending   atomic.Pointer[pendingSubmissionAdmission]
	reused    atomic.Uint64
	conflicts atomic.Uint64
	unknown   atomic.Uint64
}

// pendingSubmissionAdmission is the immutable fact persisted with turn/start.
// The execution path receives the same images through turnAdmission, so it does
// not need to consult controller-scoped temporary state.
type pendingSubmissionAdmission struct {
	receipt     session.SubmissionReceipt
	imageInputs []attachment.ImageInput
	durableCtx  context.Context
}

type turnAdmission struct {
	images     preparedImageReferences
	durableCtx context.Context
}

func newTurnAdmission(ctx context.Context, images preparedImageReferences) turnAdmission {
	if ctx == nil {
		ctx = context.Background()
	}
	return turnAdmission{images: images, durableCtx: ctx}
}

func (c *Controller) appendSubmissionEvent(events []session.Event, turnID string) []session.Event {
	if pending := c.submissions.pending.Load(); pending != nil {
		receipt := pending.receipt
		receipt.TurnID = turnID
		payload := struct {
			session.SubmissionReceipt
			ImageInputs []attachment.ImageInput `json:"imageInputs,omitempty"`
		}{SubmissionReceipt: receipt, ImageInputs: pending.imageInputs}
		data, _ := json.Marshal(payload)
		return append(events, session.Event{Kind: "submission/accepted", Optional: true, Payload: data})
	}
	return events
}

func (c *Controller) trySubmissionAdmissionLock() func() {
	if !c.submissions.mu.TryLock() {
		return nil
	}
	return sync.OnceFunc(c.submissions.mu.Unlock)
}

func (c *Controller) releaseSubmissionAdmission() {
	c.submissions.mu.Unlock()
	// A synchronous queue dispatch may have deferred while submit owned the gate.
	c.maybeDispatchInbox()
}

func submissionFingerprint(req SubmissionRequest) string {
	data, _ := json.Marshal(req)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

const submissionFingerprintVersion = 1

func canonicalSubmissionFingerprint(req SubmissionRequest) string {
	req.AttachmentDigests = nil
	for i, item := range req.Attachments {
		transport := item.Path
		if item.DraftID != "" {
			transport = "draft:" + item.DraftID
		}
		if transport != "" {
			req.Display = strings.ReplaceAll(req.Display, "("+transport+")", "(attachment:"+item.ClientAttachmentID+")")
			req.Input = canonicalAttachmentToken(req.Input, transport, item.ClientAttachmentID)
			req.Original = canonicalAttachmentToken(req.Original, transport, item.ClientAttachmentID)
		}
		if item.DraftID != "" {
			req.DraftIDs = withoutDraftID(req.DraftIDs, item.DraftID)
		}
		if i == 0 {
			req.Attachments = append([]SubmissionAttachment(nil), req.Attachments...)
		}
		req.Attachments[i] = SubmissionAttachment{ClientAttachmentID: item.ClientAttachmentID}
	}
	return submissionFingerprint(req)
}

// MatchesSubmissionReceipt validates a cold durable receipt without creating
// a Controller. Version zero keeps the exact pre-attachment fingerprint.
func MatchesSubmissionReceipt(req SubmissionRequest, receipt session.SubmissionReceipt) bool {
	if req.ID != receipt.SubmissionID {
		return false
	}
	switch receipt.FingerprintVersion {
	case 0:
		return receipt.Fingerprint == submissionFingerprint(req)
	case submissionFingerprintVersion:
		return receipt.Fingerprint == canonicalSubmissionFingerprint(req)
	default:
		return false
	}
}

// LookupSubmission also detects accidental reuse of a key for different input.
func (c *Controller) LookupSubmission(req SubmissionRequest) (session.SubmissionReceipt, bool, error) {
	return c.LookupSubmissionContext(c.attachmentContext(), req)
}

func (c *Controller) LookupSubmissionContext(ctx context.Context, req SubmissionRequest) (session.SubmissionReceipt, bool, error) {
	store := c.sessionEventStore()
	if store == nil || req.ID == "" {
		return session.SubmissionReceipt{}, false, nil
	}
	receipt, ok := store.Submission(req.ID)
	fingerprint := canonicalSubmissionFingerprint(req)
	if receipt.FingerprintVersion == 0 {
		fingerprint = submissionFingerprint(req)
	}
	if ok && receipt.FingerprintVersion > submissionFingerprintVersion {
		return receipt, true, errors.New("submission receipt version is unsupported; automatic replay is disabled")
	}
	if ok && receipt.Fingerprint != fingerprint {
		if receipt.FingerprintVersion == 0 {
			return receipt, true, errors.New("legacy submission receipt cannot be verified; automatic replay is disabled")
		}
		c.submissions.conflicts.Add(1)
		slog.Warn("submission conflict", "submissionId", req.ID)
		return receipt, true, errors.New("submission identity conflicts with different input")
	}
	if ok {
		c.submissions.reused.Add(1)
		// The projection may already contain an asynchronously accepted batch.
		// A retry is acknowledged only after its canonical journal is durable.
		if _, err := store.Flush(ctx); err != nil {
			c.submissions.unknown.Add(1)
			return receipt, true, err
		}
		slog.Debug("submission already accepted", "submissionId", req.ID, "turnId", receipt.TurnID)
	}
	return receipt, ok, nil
}

// SubmitIdentified serializes identity checking with synchronous turn admission.
func (c *Controller) SubmitIdentified(req SubmissionRequest) (session.SubmissionReceipt, error) {
	return c.SubmitIdentifiedContext(c.attachmentContext(), req)
}

func (c *Controller) SubmitIdentifiedContext(ctx context.Context, req SubmissionRequest) (session.SubmissionReceipt, error) {
	if len(req.ID) > 256 || strings.ContainsAny(req.ID, "\x00\r\n") {
		return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, errors.New("invalid submission identity"))
	}
	return c.submitIdentifiedWithSetupContext(ctx, req, nil, func(admission turnAdmission) {
		c.submitIdentifiedRequestLocked(req, admission)
	})
}

func (c *Controller) submitIdentified(req SubmissionRequest, submit func()) (session.SubmissionReceipt, error) {
	return c.submitIdentifiedWithSetup(req, nil, func(turnAdmission) { submit() })
}

// SubmitIdentifiedWithSetup validates and freezes explicit image attachments
// before setup mutates host-visible session state. Desktop uses this for the
// initial Goal transaction, whose Goal/profile changes must not survive a
// rejected image submission.
func (c *Controller) SubmitIdentifiedWithSetup(req SubmissionRequest, setup func() error) (session.SubmissionReceipt, error) {
	return c.SubmitIdentifiedWithSetupContext(c.attachmentContext(), req, setup)
}

func (c *Controller) SubmitIdentifiedWithSetupContext(ctx context.Context, req SubmissionRequest, setup func() error) (session.SubmissionReceipt, error) {
	if len(req.ID) > 256 || strings.ContainsAny(req.ID, "\x00\r\n") {
		return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, errors.New("invalid submission identity"))
	}
	return c.submitIdentifiedWithSetupContext(ctx, req, setup, func(admission turnAdmission) {
		c.submitIdentifiedRequestLocked(req, admission)
	})
}

func (c *Controller) submitIdentifiedRequestLocked(req SubmissionRequest, admission turnAdmission) {
	switch {
	case req.Action == ProtocolRecoveryAction:
		c.submitProtocolRecoveryLocked(req.RecoveryID, req.Input, admission)
	case req.Action == "delivery-recovery":
		c.submitFinalReadinessRecoveryLocked(req.Display, req.Input, admission)
	case req.Action == "shell":
		c.runShell(req.Input, admission)
	case req.HTTP:
		c.submitHTTPWithFormatLocked(req.Input, req.Display, req.Format, admission)
	case len(req.Invocations) > 0:
		c.submitInvocationsLocked(req.Input, req.Display, req.Invocations, admission)
	default:
		c.submitLocked(req.Input, req.Display, req.Original, admission)
	}
}

func (c *Controller) submitIdentifiedWithSetup(req SubmissionRequest, setup func() error, submit func(turnAdmission)) (session.SubmissionReceipt, error) {
	return c.submitIdentifiedWithSetupContext(c.attachmentContext(), req, setup, submit)
}

func (c *Controller) submitIdentifiedWithSetupContext(ctx context.Context, req SubmissionRequest, setup func() error, submit func(turnAdmission)) (session.SubmissionReceipt, error) {
	prepared, err := c.PrepareSubmission(ctx, req)
	if err != nil {
		c.notice(err.Error())
		return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, err)
	}
	return c.acceptPreparedSubmission(ctx, prepared, setup, submit)
}

func (c *Controller) acceptPreparedSubmission(ctx context.Context, candidate *PreparedSubmission, setup func() error, submit func(turnAdmission)) (session.SubmissionReceipt, error) {
	ctx, cancel := c.NewAttachmentOperationContext(ctx)
	defer cancel()
	c.submissions.mu.Lock()
	defer c.releaseSubmissionAdmission()
	req := candidate.request
	if candidate.scope != c.attachmentScope() {
		return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, errors.New("attachment target changed; please retry"))
	}
	if receipt, ok, err := c.LookupSubmissionContext(ctx, req); ok || err != nil {
		return receipt, err
	}
	prepared := candidate.images
	if err := ctx.Err(); err != nil {
		return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, err)
	}
	store := c.sessionEventStore()
	if req.ID != "" {
		if store == nil {
			return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, errors.New("durable submission identity unavailable"))
		}
		if c.Running() {
			return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, ErrTurnRunning)
		}
	}
	if setup != nil {
		if err := setup(); err != nil {
			return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, err)
		}
	}
	admission := newTurnAdmission(ctx, prepared)
	if req.ID == "" {
		submit(admission)
		return session.SubmissionReceipt{}, nil
	}
	receipt := &session.SubmissionReceipt{SessionID: store.ID(), SubmissionID: req.ID,
		FingerprintVersion: submissionFingerprintVersion, Fingerprint: canonicalSubmissionFingerprint(req), MessageID: agent.NewMessageID(), AcceptedAttachmentDigests: strings.Join(attachmentDigests(prepared), ",")}
	pending := &pendingSubmissionAdmission{
		receipt:     *receipt,
		imageInputs: append([]attachment.ImageInput(nil), prepared.inputs...),
		durableCtx:  ctx,
	}
	c.submissions.pending.Store(pending)
	defer c.submissions.pending.Store(nil)
	c.SetTurnSubmissionID(req.ID)
	submit(admission)
	if err := c.flushSubmissionAdmission(ctx); err != nil {
		c.submissions.unknown.Add(1)
		return session.SubmissionReceipt{}, err
	}
	accepted, ok := store.Submission(req.ID)
	if !ok {
		return session.SubmissionReceipt{}, errors.Join(ErrSubmissionNotAccepted, errors.New("submission was not durably admitted"))
	}
	return accepted, nil
}

func (c *Controller) submissionForTurn(turnID string) (session.SubmissionReceipt, bool) {
	store := c.sessionEventStore()
	if store == nil {
		return session.SubmissionReceipt{}, false
	}
	return store.SubmissionForTurn(turnID)
}

func (c *Controller) flushSubmissionAdmission(ctx context.Context) error {
	if c.submissions.pending.Load() == nil {
		return nil
	}
	_, err := c.sessionEventStore().Flush(ctx)
	return err
}

func (c *Controller) submissionAdmissionContext() context.Context {
	if pending := c.submissions.pending.Load(); pending != nil && pending.durableCtx != nil {
		return pending.durableCtx
	}
	return context.Background()
}

func (c *Controller) prepareSubmissionImages(req SubmissionRequest) (preparedImageReferences, []ImageReferenceFailure) {
	return c.prepareSubmissionImagesContext(c.attachmentContext(), req)
}

func (c *Controller) prepareSubmissionImagesContext(ctx context.Context, req SubmissionRequest) (preparedImageReferences, []ImageReferenceFailure) {
	ids := append([]string(nil), req.DraftIDs...)
	ids = append(ids, draftIDsFromInput(req.Input)...)
	seen := make(map[string]bool)
	unique := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	ids = unique
	prepared := preparedImageReferences{byPath: map[string]string{}}
	svc := c.attachmentService()
	sources := append([]attachment.Source(nil), req.inheritedSources...)
	structured, failures := c.structuredImageSources(ctx, req.Attachments)
	if len(failures) > 0 {
		return preparedImageReferences{}, failures
	}
	sources = append(sources, structured...)
	if len(req.Attachments) > 0 {
		filtered := ids[:0]
		for _, id := range ids {
			found := false
			for _, item := range req.Attachments {
				if item.DraftID == id {
					found = true
					break
				}
			}
			if !found {
				filtered = append(filtered, id)
			}
		}
		ids = filtered
	}
	for _, id := range ids {
		key := "draft:" + id
		ref := req.frozenSources[key]
		if ref == nil {
			draft, ok := svc.Drafts().Lookup(c.attachmentScope(), id)
			if !ok {
				return preparedImageReferences{}, imageFailuresFromAttachment(attachment.Error{Code: attachment.CodeMissing, Message: "draft credential is not valid"})
			}
			ref = &draft.Ref
		}
		sources = append(sources, attachment.Source{Existing: ref, DisplayName: ref.DisplayName, Path: key})
	}
	for _, source := range c.explicitImageSources(req.Input) {
		if frozen := req.frozenSources[normalizedImageReferencePath(source.Path)]; frozen != nil {
			source.Existing = frozen
		}
		duplicate := false
		for _, existing := range sources {
			if existing.Path != "" && normalizedImageReferencePath(existing.Path) == normalizedImageReferencePath(source.Path) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return prepared, nil
	}
	sources = c.appendOrdinaryImageSources(req.Input, sources)
	batch, err := svc.PrepareBatch(ctx, sources)
	if err != nil {
		return preparedImageReferences{}, imageFailuresFromAttachment(err)
	}
	refs, err := svc.CommitBatch(ctx, batch)
	if err != nil {
		return preparedImageReferences{}, imageFailuresFromAttachment(err)
	}
	if err := c.rebindPreparedDrafts(req, ids); err != nil {
		return preparedImageReferences{}, imageFailuresFromAttachment(err)
	}
	prepared.inputs = svc.InputsFromRefs(refs)
	for _, ref := range refs {
		prepared.ordered = append(prepared.ordered, ref.Content.Digest)
	}
	for i, source := range sources {
		if source.Path != "" {
			prepared.byPath[normalizedImageReferencePath(source.Path)] = refs[i].Content.Digest
		}
	}
	for i, item := range req.Attachments {
		prepared.byPath["attachment:"+item.ClientAttachmentID] = refs[len(req.inheritedSources)+i].Content.Digest
	}
	prepared.inputs = append(prepared.inputs, legacyRemoteImageInputs(req.Input)...)
	return prepared, nil
}

var draftIDPattern = regexp.MustCompile(`^draft:([0-9a-fA-F]{32})$`)

func draftIDsFromInput(input string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, token := range parseRefTokens(input) {
		match := draftIDPattern.FindStringSubmatch(token)
		if len(match) != 2 {
			continue
		}
		id := match[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func attachmentDigests(prepared preparedImageReferences) []string {
	if len(prepared.inputs) == 0 {
		return nil
	}
	out := make([]string, 0, len(prepared.inputs))
	for _, in := range prepared.inputs {
		if in.Kind == attachment.KindAttachment && in.Attachment != nil {
			out = append(out, in.Attachment.Content.Digest)
		}
	}
	return out
}

func (c *Controller) flushSubmissionStart(ctx context.Context, kind event.Kind) error {
	if kind != event.TurnStarted {
		return nil
	}
	return c.flushSubmissionAdmission(ctx)
}

// TurnIDForSubmission exposes the synchronous admission receipt without
// depending on whether the provider is still running when the desktop call returns.
func (c *Controller) TurnIDForSubmission(submissionID string) string {
	if store := c.sessionEventStore(); store != nil {
		if receipt, ok := store.Submission(submissionID); ok {
			return receipt.TurnID
		}
	}
	ledger := c.turnEventLedger()
	if ledger == nil {
		return ""
	}
	return ledger.TurnIDForSubmission(submissionID)
}
