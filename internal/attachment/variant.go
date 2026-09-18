package attachment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"sync"

	xdraw "golang.org/x/image/draw"
	"reasonix/internal/sessioncontent"
)

type variantKey struct {
	digest  string
	version int
	width   int
	height  int
	format  string
	quality int
}

type cacheEntry struct {
	key     variantKey
	size    int64
	result  Variant
	witness sessioncontent.ObjectWitness
}

// VariantCache is a process-local LRU of request encodings. It never stores
// or deletes persistent originals.
type VariantCache struct {
	mu         sync.Mutex
	maxBytes   int64
	used       int64
	entries    map[variantKey]*cacheEntry
	order      []*cacheEntry
	transforms chan struct{}
	inflight   map[variantKey]*sharedTransform
}

type sharedTransform struct {
	waiters int
	cancel  context.CancelFunc
	done    chan struct{}
	result  Variant
	err     error
	witness sessioncontent.ObjectWitness
}

func NewVariantCache(maxBytes int64, transforms int) *VariantCache {
	if maxBytes <= 0 {
		maxBytes = DefaultCacheBytes
	}
	if transforms <= 0 {
		transforms = DefaultTransforms
	}
	return &VariantCache{
		maxBytes:   maxBytes,
		entries:    map[variantKey]*cacheEntry{},
		transforms: make(chan struct{}, transforms),
		inflight:   map[variantKey]*sharedTransform{},
	}
}

func (s *Service) PrepareVariant(ctx context.Context, ref AttachmentRef, policyVersion int) (Variant, error) {
	if s == nil || s.cache == nil {
		return Variant{}, Error{Code: CodeUnreadable, Message: "attachment variant cache unavailable"}
	}
	key, err := variantCacheKeyFromRef(ref, policyVersion)
	if err != nil {
		return Variant{}, err
	}
	witness, err := s.store.Probe(ctx, ref.Content)
	if err != nil {
		return Variant{}, err
	}
	if hit, cachedWitness, ok := s.cache.getWithWitness(key); ok {
		if cachedWitness.Same(witness) {
			if err := verifyVariant(hit, key); err == nil {
				return cloneVariant(hit), nil
			}
		}
		s.cache.remove(key)
	}
	return s.cache.shareWithWitness(ctx, key, witness, func(workCtx context.Context) (Variant, error) {
		raw, err := s.ReadVerified(workCtx, ref)
		if err != nil {
			return Variant{}, err
		}
		verifiedKey, err := variantCacheKey(ref, raw, policyVersion)
		if err != nil {
			return Variant{}, err
		}
		if verifiedKey != key {
			return Variant{}, Error{Code: CodeChanged, Name: ref.DisplayName, Message: "attachment metadata changed during validation"}
		}
		return encodeVariant(workCtx, ref, raw, policyVersion)
	})
}

func variantCacheKeyFromRef(ref AttachmentRef, policyVersion int) (variantKey, error) {
	if policyVersion == 0 {
		policyVersion = VariantPolicyV1
	}
	if policyVersion != VariantPolicyV1 {
		return variantKey{}, Error{Code: CodeUnsupported, Name: ref.DisplayName, Message: "unsupported image transform policy"}
	}
	if err := ref.Validate(); err != nil {
		return variantKey{}, err
	}
	w, h := scaledDims(ref.Width, ref.Height, VariantMaxDim)
	format, quality := "original", 0
	if w != ref.Width || h != ref.Height {
		if ref.MIME() == "image/jpeg" {
			format, quality = "jpeg", VariantJPEGQuality
		} else {
			format = "png"
		}
	}
	return variantKey{digest: ref.Content.Digest, version: policyVersion, width: w, height: h, format: format, quality: quality}, nil
}

func (c *VariantCache) Prepare(ctx context.Context, ref AttachmentRef, raw []byte, policyVersion int) (Variant, error) {
	if policyVersion == 0 {
		policyVersion = VariantPolicyV1
	}
	if policyVersion != VariantPolicyV1 {
		return Variant{}, Error{Code: CodeUnsupported, Name: ref.DisplayName, Message: "unsupported image transform policy"}
	}
	if err := ctx.Err(); err != nil {
		return Variant{}, canceledError(err)
	}
	key, err := variantCacheKey(ref, raw, policyVersion)
	if err != nil {
		return Variant{}, err
	}
	if hit, ok := c.get(key); ok {
		if err := verifyVariant(hit, key); err != nil {
			c.remove(key)
		} else {
			return cloneVariant(hit), nil
		}
	}
	return c.share(ctx, key, func(workCtx context.Context) (Variant, error) {
		return encodeVariant(workCtx, ref, raw, policyVersion)
	})
}

func variantCacheKey(ref AttachmentRef, raw []byte, policyVersion int) (variantKey, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return variantKey{}, Error{Code: CodeCorrupt, Name: ref.DisplayName, Message: defaultDetail(CodeCorrupt), Cause: err}
	}
	w, h := scaledDims(cfg.Width, cfg.Height, VariantMaxDim)
	format, quality := "original", 0
	if w != cfg.Width || h != cfg.Height {
		if ref.MIME() == "image/jpeg" {
			format, quality = "jpeg", VariantJPEGQuality
		} else {
			format, quality = "png", 0
		}
	}
	return variantKey{digest: ref.Content.Digest, version: policyVersion, width: w, height: h, format: format, quality: quality}, nil
}

func encodeVariant(ctx context.Context, ref AttachmentRef, raw []byte, policyVersion int) (Variant, error) {
	if err := ctx.Err(); err != nil {
		return Variant{}, canceledError(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Variant{}, Error{Code: CodeCorrupt, Name: ref.DisplayName, Message: defaultDetail(CodeCorrupt), Cause: err}
	}
	w, h := scaledDims(cfg.Width, cfg.Height, VariantMaxDim)
	if w == cfg.Width && h == cfg.Height {
		return variantFromOriginal(ref, raw, cfg.Width, cfg.Height, policyVersion), nil
	}
	if err := ctx.Err(); err != nil {
		return Variant{}, canceledError(err)
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Variant{}, Error{Code: CodeCorrupt, Name: ref.DisplayName, Message: defaultDetail(CodeCorrupt), Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return Variant{}, canceledError(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	if err := ctx.Err(); err != nil {
		return Variant{}, canceledError(err)
	}
	var buf bytes.Buffer
	mime := "image/png"
	if ref.MIME() == "image/jpeg" && !hasAlpha(src) {
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: VariantJPEGQuality}); err != nil {
			return Variant{}, Error{Code: CodeCorrupt, Name: ref.DisplayName, Message: "could not encode the image variant", Cause: err}
		}
		mime = "image/jpeg"
	} else if err := png.Encode(&buf, dst); err != nil {
		return Variant{}, Error{Code: CodeCorrupt, Name: ref.DisplayName, Message: "could not encode the image variant", Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return Variant{}, canceledError(err)
	}
	out := buf.Bytes()
	sum := sha256.Sum256(out)
	return Variant{
		Bytes:         append([]byte(nil), out...),
		MIME:          mime,
		Width:         w,
		Height:        h,
		SourceWidth:   cfg.Width,
		SourceHeight:  cfg.Height,
		PolicyVersion: policyVersion,
		Digest:        hex.EncodeToString(sum[:]),
	}, nil
}

func variantFromOriginal(ref AttachmentRef, raw []byte, width, height, policyVersion int) Variant {
	sum := sha256.Sum256(raw)
	return Variant{
		Bytes:         append([]byte(nil), raw...),
		MIME:          ref.MIME(),
		Width:         width,
		Height:        height,
		SourceWidth:   width,
		SourceHeight:  height,
		PolicyVersion: policyVersion,
		Digest:        hex.EncodeToString(sum[:]),
	}
}

func scaledDims(w, h, limit int) (int, int) {
	if w <= 0 || h <= 0 || limit <= 0 || (w <= limit && h <= limit) {
		return maxInt(w, 1), maxInt(h, 1)
	}
	if w >= h {
		return limit, maxInt(h*limit/w, 1)
	}
	return maxInt(w*limit/h, 1), limit
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (c *VariantCache) share(ctx context.Context, key variantKey, work func(context.Context) (Variant, error)) (Variant, error) {
	return c.shareWithWitness(ctx, key, sessioncontent.ObjectWitness{}, work)
}

func (c *VariantCache) shareWithWitness(ctx context.Context, key variantKey, witness sessioncontent.ObjectWitness, work func(context.Context) (Variant, error)) (Variant, error) {
	if err := ctx.Err(); err != nil {
		return Variant{}, canceledError(err)
	}
	c.mu.Lock()
	if hit, ok := c.entries[key]; ok {
		c.touchLocked(hit)
		result := cloneVariant(hit.result)
		c.mu.Unlock()
		return result, nil
	}
	shared, ok := c.inflight[key]
	if ok && witness.Valid() && shared.witness.Valid() && !shared.witness.Same(witness) {
		delete(c.inflight, key)
		shared.cancel()
		shared, ok = nil, false
	}
	if !ok {
		workCtx, cancel := context.WithCancel(context.Background())
		shared = &sharedTransform{cancel: cancel, done: make(chan struct{}), witness: witness}
		c.inflight[key] = shared
		go func() {
			select {
			case c.transforms <- struct{}{}:
			case <-workCtx.Done():
				c.finish(key, shared, Variant{}, witness, canceledError(workCtx.Err()))
				return
			}
			if err := workCtx.Err(); err != nil {
				<-c.transforms
				c.finish(key, shared, Variant{}, witness, canceledError(err))
				return
			}
			result, err := work(workCtx)
			<-c.transforms
			if err == nil {
				if pubErr := workCtx.Err(); pubErr != nil {
					err = canceledError(pubErr)
					result = Variant{}
				}
			}
			c.finish(key, shared, result, witness, err)
		}()
	}
	shared.waiters++
	c.mu.Unlock()
	defer c.leave(key, shared)

	select {
	case <-ctx.Done():
		return Variant{}, canceledError(ctx.Err())
	case <-shared.done:
		if err := ctx.Err(); err != nil {
			return Variant{}, canceledError(err)
		}
		if shared.err != nil {
			return Variant{}, shared.err
		}
		return cloneVariant(shared.result), nil
	}
}

func (c *VariantCache) finish(key variantKey, shared *sharedTransform, result Variant, witness sessioncontent.ObjectWitness, err error) {
	c.mu.Lock()
	if err == nil && shared.waiters > 0 && c.inflight[key] == shared {
		c.putLocked(key, result, witness)
	}
	shared.result = result
	shared.err = err
	close(shared.done)
	if c.inflight[key] == shared {
		delete(c.inflight, key)
	}
	c.mu.Unlock()
}

func (c *VariantCache) leave(key variantKey, shared *sharedTransform) {
	c.mu.Lock()
	defer c.mu.Unlock()
	shared.waiters--
	if shared.waiters <= 0 {
		if c.inflight[key] == shared {
			delete(c.inflight, key)
		}
		shared.cancel()
	}
}

func (c *VariantCache) get(key variantKey) (Variant, bool) {
	result, _, ok := c.getWithWitness(key)
	return result, ok
}

func (c *VariantCache) getWithWitness(key variantKey) (Variant, sessioncontent.ObjectWitness, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return Variant{}, sessioncontent.ObjectWitness{}, false
	}
	c.touchLocked(entry)
	return cloneVariant(entry.result), entry.witness, true
}

func (c *VariantCache) putLocked(key variantKey, result Variant, witness sessioncontent.ObjectWitness) {
	if _, ok := c.entries[key]; ok {
		return
	}
	entry := &cacheEntry{key: key, size: int64(len(result.Bytes)), result: cloneVariant(result), witness: witness}
	c.entries[key] = entry
	c.order = append(c.order, entry)
	c.used += entry.size
	c.evictLocked()
}

func (c *VariantCache) remove(key variantKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return
	}
	delete(c.entries, key)
	c.used -= entry.size
	for i, item := range c.order {
		if item == entry {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *VariantCache) touchLocked(entry *cacheEntry) {
	for i, item := range c.order {
		if item == entry {
			c.order = append(append(c.order[:i], c.order[i+1:]...), entry)
			return
		}
	}
}

func (c *VariantCache) evictLocked() {
	for c.used > c.maxBytes && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest.key)
		c.used -= oldest.size
	}
}

func (c *VariantCache) Used() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}

func cloneVariant(in Variant) Variant {
	out := in
	if len(in.Bytes) > 0 {
		out.Bytes = append([]byte(nil), in.Bytes...)
	}
	return out
}

func verifyVariant(in Variant, key variantKey) error {
	if int64(len(in.Bytes)) == 0 || in.Digest == "" {
		return fmt.Errorf("empty variant")
	}
	sum := sha256.Sum256(in.Bytes)
	if hex.EncodeToString(sum[:]) != in.Digest {
		return fmt.Errorf("variant digest mismatch")
	}
	if in.PolicyVersion != key.version || in.Width != key.width || in.Height != key.height {
		return fmt.Errorf("variant identity mismatch")
	}
	return nil
}
