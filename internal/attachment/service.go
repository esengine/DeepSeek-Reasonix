package attachment

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"maps"
	"sync"

	"reasonix/internal/sessioncontent"
)

// Service validates images and publishes originals into a session content store.
type Service struct {
	store  *sessioncontent.Store
	policy Policy
	cache  *VariantCache
	drafts *DraftStore
}

func NewService(store *sessioncontent.Store, cache *VariantCache) *Service {
	if cache == nil {
		cache = NewVariantCache(DefaultCacheBytes, DefaultTransforms)
	}
	return &Service{store: store, policy: DefaultPolicy(), cache: cache, drafts: NewDraftStore()}
}

func (s *Service) WithPolicy(policy Policy) *Service {
	if s == nil {
		return nil
	}
	out := *s
	out.policy = policy.withDefaults()
	return &out
}

func (s *Service) Store() *sessioncontent.Store {
	if s == nil {
		return nil
	}
	return s.store
}

func (s *Service) Drafts() *DraftStore {
	if s == nil {
		return nil
	}
	return s.drafts
}

func (s *Service) Cache() *VariantCache {
	if s == nil {
		return nil
	}
	return s.cache
}

func (s *Service) PrepareBatch(ctx context.Context, sources []Source) (PreparedImages, error) {
	if s == nil {
		return PreparedImages{}, Error{Code: CodeUnreadable, Message: "attachment service unavailable"}
	}
	if err := ctx.Err(); err != nil {
		return PreparedImages{}, canceledError(err)
	}
	policy := s.policy.withDefaults()
	if len(sources) > policy.MaxCount {
		return PreparedImages{}, Error{Code: CodeTooMany, Message: defaultDetail(CodeTooMany)}
	}
	out := PreparedImages{Items: make([]PreparedImage, 0, len(sources))}
	var failures BatchError
	var total int64
	for i, source := range sources {
		if err := ctx.Err(); err != nil {
			return PreparedImages{}, canceledError(err)
		}
		item, err := s.prepareSource(ctx, source, policy)
		if err != nil {
			itemErr := asError(err)
			itemErr.Index = i + 1
			if itemErr.Name == "" {
				itemErr.Name = source.displayName()
			}
			failures = append(failures, itemErr)
			continue
		}
		size := int64(len(item.Bytes))
		if item.Existing != nil {
			size = item.Existing.Content.Bytes
		}
		total += size
		if total > policy.MaxBatchBytes {
			failures = append(failures, Error{Code: CodeBatchSize, Name: item.DisplayName, Index: i + 1, Message: defaultDetail(CodeBatchSize)})
			continue
		}
		out.Items = append(out.Items, item)
	}
	if len(failures) > 0 {
		return PreparedImages{}, failures
	}
	return out, nil
}

func (s *Service) prepareSource(ctx context.Context, source Source, policy Policy) (PreparedImage, error) {
	select {
	case s.cache.transforms <- struct{}{}:
		defer func() { <-s.cache.transforms }()
	case <-ctx.Done():
		return PreparedImage{}, canceledError(ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return PreparedImage{}, canceledError(err)
	}
	if source.Existing == nil {
		return source.Read(ctx, policy)
	}
	ref := source.Existing
	if ref.Content.Bytes > policy.MaxBytes {
		return PreparedImage{}, Error{Code: CodeSize, Message: defaultDetail(CodeSize)}
	}
	raw, err := s.ReadVerified(ctx, *ref)
	if err != nil {
		return PreparedImage{}, err
	}
	item, err := (Source{Bytes: raw, DeclaredMIME: ref.MIME(), DisplayName: source.displayName()}).Read(ctx, policy)
	if err != nil {
		return PreparedImage{}, err
	}
	if item.Width != ref.Width || item.Height != ref.Height {
		return PreparedImage{}, Error{Code: CodeCorrupt, Message: defaultDetail(CodeCorrupt)}
	}
	item.Existing = ref
	item.Bytes = nil
	return item, nil
}

func (s *Service) CommitBatch(ctx context.Context, prepared PreparedImages) ([]AttachmentRef, error) {
	if s == nil || s.store == nil {
		return nil, Error{Code: CodeUnreadable, Message: "attachment store unavailable"}
	}
	if err := ctx.Err(); err != nil {
		return nil, canceledError(err)
	}
	refs := make([]AttachmentRef, 0, len(prepared.Items))
	for i, item := range prepared.Items {
		if err := ctx.Err(); err != nil {
			return nil, canceledError(err)
		}
		if item.Existing != nil {
			if err := s.store.Verify(ctx, item.Existing.Content); err != nil {
				return nil, Error{Code: CodeCorrupt, Name: item.DisplayName, Index: i + 1, Message: defaultDetail(CodeCorrupt), Cause: err}
			}
			refs = append(refs, *item.Existing)
			continue
		}
		ref, err := s.store.Put(ctx, bytes.NewReader(item.Bytes), sessioncontent.Metadata{MediaType: item.MIME, Name: item.DisplayName})
		if err != nil {
			return nil, Error{Code: CodeUnreadable, Name: item.DisplayName, Index: i + 1, Message: "could not persist the image", Cause: err, Retry: true}
		}
		refs = append(refs, AttachmentRef{
			Version:     RefVersion,
			Content:     ref,
			Width:       item.Width,
			Height:      item.Height,
			DisplayName: item.DisplayName,
		})
	}
	return refs, nil
}

func (s *Service) ReadVerified(ctx context.Context, ref AttachmentRef) ([]byte, error) {
	if s == nil || s.store == nil {
		return nil, Error{Code: CodeUnreadable, Message: "attachment store unavailable"}
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	f, err := s.store.Open(ctx, ref.Content)
	if err != nil {
		return nil, Error{Code: CodeUnreadable, Name: ref.DisplayName, Message: defaultDetail(CodeUnreadable), Cause: err}
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, Error{Code: CodeUnreadable, Name: ref.DisplayName, Message: defaultDetail(CodeUnreadable), Cause: err}
	}
	if int64(len(raw)) != ref.Content.Bytes {
		return nil, Error{Code: CodeCorrupt, Name: ref.DisplayName, Message: defaultDetail(CodeCorrupt)}
	}
	return raw, nil
}

func (s *Service) InputsFromRefs(refs []AttachmentRef) []ImageInput {
	out := make([]ImageInput, 0, len(refs))
	for i := range refs {
		ref := refs[i]
		out = append(out, ImageInput{Kind: KindAttachment, Attachment: &ref})
	}
	return out
}

func asError(err error) Error {
	var item Error
	if err != nil && errorsAs(err, &item) {
		return item
	}
	return Error{Code: CodeUnreadable, Message: defaultDetail(CodeUnreadable), Cause: err, Retry: true}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("attachment: random id: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// DraftStore issues opaque draft credentials bound to a caller-chosen key.
type DraftStore struct {
	mu    sync.Mutex
	items map[string]draftRecord
}

type draftRecord struct {
	scope       string
	draft       DraftCredential
	family      string
	needsRebind bool
}

func NewDraftStore() *DraftStore {
	return &DraftStore{items: map[string]draftRecord{}}
}

func (d *DraftStore) Issue(scope string, ref AttachmentRef) DraftCredential {
	if d == nil {
		d = NewDraftStore()
	}
	draft := DraftCredential{
		ID:          newID(),
		Ref:         ref,
		DisplayName: ref.DisplayName,
		MIME:        ref.MIME(),
		Width:       ref.Width,
		Height:      ref.Height,
		Bytes:       ref.Content.Bytes,
	}
	d.mu.Lock()
	d.items[draft.ID] = draftRecord{scope: scope, draft: draft, family: draft.ID}
	d.mu.Unlock()
	return draft
}

func (d *DraftStore) Lookup(scope, id string) (DraftCredential, bool) {
	if d == nil {
		return DraftCredential{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	item, ok := d.items[id]
	if !ok || item.scope != scope {
		return DraftCredential{}, false
	}
	return item.draft, true
}

func (d *DraftStore) Resolve(scope string, ids []string) ([]AttachmentRef, error) {
	refs := make([]AttachmentRef, 0, len(ids))
	for i, id := range ids {
		draft, ok := d.Lookup(scope, id)
		if !ok {
			return nil, Error{Code: CodeMissing, Name: "image", Index: i + 1, Message: "draft credential is not valid", Retry: true}
		}
		refs = append(refs, draft.Ref)
	}
	return refs, nil
}

func (d *DraftStore) Release(scope, id string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	item, ok := d.items[id]
	if ok && item.scope == scope {
		for key, related := range d.items {
			if related.scope == scope && related.family == item.family {
				delete(d.items, key)
			}
		}
	}
}

func (d *DraftStore) ReleaseScope(scope string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, item := range d.items {
		if item.scope == scope {
			delete(d.items, id)
		}
	}
}

// CopyScopeTo transfers credentials during an owner-authorized replacement.
// It cannot grant access to another session or storage generation.
func (d *DraftStore) CopyScopeTo(scope string, target *DraftStore) {
	if d == nil || target == nil || d == target {
		return
	}
	d.mu.Lock()
	items := make(map[string]draftRecord)
	for id, item := range d.items {
		if item.scope == scope {
			item.needsRebind = true
			items[id] = item
		}
	}
	d.mu.Unlock()
	target.mu.Lock()
	defer target.mu.Unlock()
	maps.Copy(target.items, items)
}
