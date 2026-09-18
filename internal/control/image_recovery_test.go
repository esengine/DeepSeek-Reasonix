package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/tool"
)

type cancelAfterFirstErrContext struct {
	context.Context
	mu    sync.Mutex
	calls int
}

func (c *cancelAfterFirstErrContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

func newImageRecoveryController(t *testing.T, id string, persistence session.SessionPersistence) (*Controller, *session.Runtime, string) {
	t.Helper()
	root := ""
	if persistence == nil {
		root = filepath.Join(t.TempDir(), "sessions")
		persistence = session.NewFilesystemPersistence(root)
	}
	service, err := session.NewService("desktop", persistence)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	controller := newOwnedTestController(t, Options{
		Executor: exec, Sink: event.Discard, SessionService: service,
		SessionRuntime: runtime, ExclusiveSession: true,
	})
	return controller, runtime, root
}

func imageRecoveryFixture(t *testing.T, runtime *session.Runtime) (*provider.ImageRecoveryAction, provider.ImageIsolationScope, []string) {
	t.Helper()
	images := []string{"data:image/png;base64,AAAA", "data:image/png;base64,BBBB"}
	message := provider.Message{ID: "image-message", Role: provider.RoleUser, Content: "inspect", Images: images}
	payload, err := marshalSessionMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "seed-image-message", []session.Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	candidates := make([]provider.ImageRecoveryCandidate, 0, len(images))
	for ordinal, image := range images {
		candidates = append(candidates, provider.ImageRecoveryCandidate{Identity: provider.ImageIdentity{
			MessageID: message.ID, ImageOrdinal: ordinal, ContentDigest: provider.ImageContentDigest(image),
		}})
	}
	return &provider.ImageRecoveryAction{ID: "manual-recovery", Reason: "invalid_image", Candidates: candidates}, provider.ImageIsolationScope{Kind: "content"}, images
}

func marshalSessionMessage(message provider.Message) ([]byte, error) {
	return json.Marshal(map[string]any{"message": message})
}

func TestResolveImageRecoveryPersistsOnlySelectedImageWithoutReplay(t *testing.T) {
	controller, runtime, _ := newImageRecoveryController(t, "manual-image-recovery", nil)
	action, scope, images := imageRecoveryFixture(t, runtime)
	controller.setPendingImageRecovery(action, scope)

	if err := controller.ResolveImageRecovery(t.Context(), action.ID, nil); err == nil {
		t.Fatal("empty selection was accepted")
	}
	foreign := provider.ImageIdentity{MessageID: "other", ImageOrdinal: 0, ContentDigest: provider.ImageContentDigest("other")}
	if err := controller.ResolveImageRecovery(t.Context(), action.ID, []provider.ImageIdentity{foreign}); err == nil {
		t.Fatal("foreign selection was accepted")
	}
	selected := action.Candidates[1].Identity
	before := runtime.Session().Snapshot().EventSequence
	if err := controller.ResolveImageRecovery(t.Context(), action.ID, []provider.ImageIdentity{selected}); err != nil {
		t.Fatal(err)
	}
	after := runtime.Session().Snapshot()
	if after.EventSequence != before+1 {
		t.Fatalf("event sequence=%d want=%d", after.EventSequence, before+1)
	}
	if runtime.Session().Manifest().StorageRevision != session.MaxStorageRevision {
		t.Fatalf("storage revision manifest=%d", runtime.Session().Manifest().StorageRevision)
	}
	if controller.PendingImageRecovery() != nil {
		t.Fatal("durable recovery remained pending")
	}
	if len(after.Projection.ImageIsolations) != 1 {
		t.Fatalf("isolations=%#v", after.Projection.ImageIsolations)
	}
	projected, changed := provider.ApplyImageIsolation(after.Projection.ModelMessages, []provider.ImageIsolationDecision{{
		Version: 1, Identity: selected, Reason: action.Reason, Scope: scope,
	}}, provider.ImageRequestIdentity{})
	var projectedUser *provider.Message
	for i := range projected {
		if projected[i].ID == "image-message" {
			projectedUser = &projected[i]
			break
		}
	}
	if !changed || projectedUser == nil || len(projectedUser.Images) != 1 || projectedUser.Images[0] != images[0] {
		t.Fatalf("selected isolation projection=%#v changed=%v", projected, changed)
	}
	var canonicalUser *provider.Message
	for i := range after.Projection.Messages {
		if after.Projection.Messages[i].ID == "image-message" {
			canonicalUser = &after.Projection.Messages[i]
			break
		}
	}
	if canonicalUser == nil || len(canonicalUser.Images) != 2 || canonicalUser.Images[0] != images[0] || canonicalUser.Images[1] != images[1] {
		t.Fatalf("canonical history changed: %#v", after.Projection.Messages)
	}
	if err := controller.ResolveImageRecovery(t.Context(), action.ID, []provider.ImageIdentity{selected}); !errors.Is(err, ErrImageRecoveryUnavailable) {
		t.Fatalf("duplicate resolved action=%v", err)
	}

	controller.setPendingImageRecovery(action, scope)
	if err := controller.ResolveImageRecovery(t.Context(), action.ID, []provider.ImageIdentity{selected}); err != nil {
		t.Fatal(err)
	}
	if got := runtime.Session().Snapshot().EventSequence; got != after.EventSequence {
		t.Fatalf("idempotent confirmation appended sequence=%d want=%d", got, after.EventSequence)
	}
}

func TestResolveImageRecoveryManifestFailureAppendsNoEvent(t *testing.T) {
	controller, runtime, root := newImageRecoveryController(t, "manifest-failure", nil)
	action, scope, _ := imageRecoveryFixture(t, runtime)
	controller.setPendingImageRecovery(action, scope)
	before := runtime.Session().Snapshot().EventSequence
	manifestPath := filepath.Join(root, runtime.Ref().SessionID, "manifest.json")
	backupPath := manifestPath + ".backup"
	if err := os.Rename(manifestPath, backupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifestPath, 0o700); err != nil {
		t.Fatal(err)
	}
	err := controller.ResolveImageRecovery(t.Context(), action.ID, []provider.ImageIdentity{action.Candidates[0].Identity})
	if err == nil {
		t.Fatal("manifest failure was ignored")
	}
	if got := runtime.Session().Snapshot().EventSequence; got != before {
		t.Fatalf("manifest failure appended event sequence=%d want=%d", got, before)
	}
	if controller.PendingImageRecovery() == nil {
		t.Fatal("manifest failure cleared pending recovery")
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backupPath, manifestPath); err != nil {
		t.Fatal(err)
	}
}

func TestResolveImageRecoveryAppendCancellationLeavesSafeRevisionFour(t *testing.T) {
	controller, runtime, _ := newImageRecoveryController(t, "append-cancel", nil)
	action, scope, _ := imageRecoveryFixture(t, runtime)
	controller.setPendingImageRecovery(action, scope)
	before := runtime.Session().Snapshot().EventSequence
	ctx := &cancelAfterFirstErrContext{Context: context.Background()}
	err := controller.ResolveImageRecovery(ctx, action.ID, []provider.ImageIdentity{action.Candidates[0].Identity})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("append cancellation=%v", err)
	}
	if runtime.Session().Manifest().StorageRevision != session.MaxStorageRevision {
		t.Fatalf("manifest revision=%d", runtime.Session().Manifest().StorageRevision)
	}
	if got := runtime.Session().Snapshot().EventSequence; got != before {
		t.Fatalf("cancelled append sequence=%d want=%d", got, before)
	}
	if controller.PendingImageRecovery() == nil {
		t.Fatal("cancelled append cleared pending recovery")
	}
}

func TestResolveImageRecoveryFlushFailureKeepsPending(t *testing.T) {
	store, err := session.CreateWithOptions(filepath.Join(t.TempDir(), "flush-failure"), "flush-failure", session.OpenOptions{
		Sync: func(*os.File) error { return errors.New("injected sync failure") },
	})
	if err != nil {
		t.Fatal(err)
	}
	controller, runtime, _ := newImageRecoveryController(t, "flush-failure", failingFlushPersistence{session: store})
	action, scope, _ := imageRecoveryFixture(t, runtime)
	controller.setPendingImageRecovery(action, scope)
	err = controller.ResolveImageRecovery(t.Context(), action.ID, []provider.ImageIdentity{action.Candidates[0].Identity})
	if err == nil {
		t.Fatal("flush failure was ignored")
	}
	if controller.PendingImageRecovery() == nil {
		t.Fatal("flush failure cleared pending recovery")
	}
	if runtime.Session().Manifest().StorageRevision != session.MaxStorageRevision {
		t.Fatalf("manifest revision=%d", runtime.Session().Manifest().StorageRevision)
	}
}
