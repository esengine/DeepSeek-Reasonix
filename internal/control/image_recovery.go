package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/provider"
	"reasonix/internal/session"
)

var ErrImageRecoveryUnavailable = errors.New("image recovery is no longer available")

type imageRecoveryState struct {
	mu     sync.Mutex
	action *provider.ImageRecoveryAction
	scope  provider.ImageIsolationScope
}

// ImageRecoveryControl is optional so older hosts and lightweight controller
// doubles do not have to implement the recovery UI mutation.
type ImageRecoveryControl interface {
	ResolveImageRecovery(context.Context, string, []provider.ImageIdentity) error
	PendingImageRecovery() *provider.ImageRecoveryAction
}

func cloneImageRecoveryAction(action *provider.ImageRecoveryAction) *provider.ImageRecoveryAction {
	if action == nil {
		return nil
	}
	clone := *action
	clone.Candidates = append([]provider.ImageRecoveryCandidate(nil), action.Candidates...)
	return &clone
}

func (c *Controller) setPendingImageRecovery(action *provider.ImageRecoveryAction, scope provider.ImageIsolationScope) {
	if c == nil || action == nil {
		return
	}
	c.imageRecovery.mu.Lock()
	c.imageRecovery.action = cloneImageRecoveryAction(action)
	c.imageRecovery.scope = scope
	c.imageRecovery.mu.Unlock()
}

func (c *Controller) PendingImageRecovery() *provider.ImageRecoveryAction {
	if c == nil {
		return nil
	}
	c.imageRecovery.mu.Lock()
	defer c.imageRecovery.mu.Unlock()
	return cloneImageRecoveryAction(c.imageRecovery.action)
}

// ResolveImageRecovery persists exactly the user-selected candidates and does
// not replay the failed user request. A later user turn observes the decisions.
func (c *Controller) ResolveImageRecovery(ctx context.Context, id string, selected []provider.ImageIdentity) error {
	if c == nil {
		return ErrImageRecoveryUnavailable
	}
	c.imageRecovery.mu.Lock()
	defer c.imageRecovery.mu.Unlock()
	pending := c.imageRecovery.action
	if pending == nil || strings.TrimSpace(id) == "" || id != pending.ID {
		return ErrImageRecoveryUnavailable
	}
	if len(selected) == 0 {
		return errors.New("select at least one image to isolate")
	}
	allowed := make(map[provider.ImageIdentity]bool, len(pending.Candidates))
	for _, candidate := range pending.Candidates {
		allowed[candidate.Identity] = true
	}
	seen := make(map[provider.ImageIdentity]bool, len(selected))
	decisions := make([]provider.ImageIsolationDecision, 0, len(selected))
	for _, identity := range selected {
		if !allowed[identity] {
			return errors.New("image recovery selection is not part of the failed request")
		}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		decisions = append(decisions, provider.ImageIsolationDecision{
			Version: 1, Identity: identity, Reason: pending.Reason, Scope: c.imageRecovery.scope,
		})
	}
	if len(decisions) == 0 {
		return errors.New("select at least one image to isolate")
	}
	if err := c.recordSessionImageIsolations(ctx, decisions); err != nil {
		return err
	}
	c.imageRecovery.action = nil
	c.imageRecovery.scope = provider.ImageIsolationScope{}
	return nil
}

func (c *Controller) recordSessionImageIsolations(ctx context.Context, decisions []provider.ImageIsolationDecision) error {
	if !c.sessionEventCommitAllowed() {
		return session.ErrStaleExecution
	}
	store := c.sessionEventStore()
	if store == nil {
		return errors.New("record image isolation: session store unavailable")
	}
	events := make([]session.Event, 0, len(decisions))
	keys := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		payload, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		events = append(events, session.Event{Kind: "model/image-isolation", Required: true, Payload: payload})
		keys = append(keys, provider.ImageDecisionKey(decision))
	}
	sort.Strings(keys)

	c.turnEvents.commitMu.Lock()
	if !c.messageCommitAllowedLocked(ctx, store) {
		c.turnEvents.commitMu.Unlock()
		return session.ErrStaleExecution
	}
	if err := store.EnsureStorageRevision(ctx, session.MaxStorageRevision); err != nil {
		c.turnEvents.commitMu.Unlock()
		return fmt.Errorf("record image isolation manifest: %w", err)
	}
	keyHash := sha256.Sum256([]byte(strings.Join(keys, "\x00")))
	commit, err := c.appendSessionBatch(ctx, store, session.Batch{
		OperationID: "image-isolation-manual:" + hex.EncodeToString(keyHash[:]),
		Events:      events,
	})
	c.turnEvents.commitMu.Unlock()
	if err != nil {
		return err
	}
	receipt, err := store.Flush(ctx)
	if err != nil {
		return err
	}
	if receipt.DurableSequence < commit.LastSequence() {
		return fmt.Errorf("record image isolation: durable sequence %d is before commit %d", receipt.DurableSequence, commit.LastSequence())
	}
	return nil
}

// RecordSessionImageIsolation upgrades the manifest before accepting the first
// required isolation event. A successful return means the decision is durable.
func (c *Controller) RecordSessionImageIsolation(ctx context.Context, decision provider.ImageIsolationDecision) error {
	if !c.sessionEventCommitAllowed() {
		return session.ErrStaleExecution
	}
	store := c.sessionEventStore()
	if store == nil {
		return errors.New("record image isolation: session store unavailable")
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return err
	}

	c.turnEvents.commitMu.Lock()
	if !c.messageCommitAllowedLocked(ctx, store) {
		c.turnEvents.commitMu.Unlock()
		return session.ErrStaleExecution
	}
	if err := store.EnsureStorageRevision(ctx, session.MaxStorageRevision); err != nil {
		c.turnEvents.commitMu.Unlock()
		return fmt.Errorf("record image isolation manifest: %w", err)
	}
	commit, err := c.appendSessionBatch(ctx, store, session.Batch{
		OperationID: "image-isolation:" + provider.ImageDecisionKey(decision),
		Events:      []session.Event{{Kind: "model/image-isolation", Required: true, Payload: payload}},
	})
	c.turnEvents.commitMu.Unlock()
	if err != nil {
		return err
	}
	receipt, err := store.Flush(ctx)
	if err != nil {
		return err
	}
	if receipt.DurableSequence < commit.LastSequence() {
		return fmt.Errorf("record image isolation: durable sequence %d is before commit %d", receipt.DurableSequence, commit.LastSequence())
	}
	return nil
}
