package attachment

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAttachmentParentSymlinkCannotEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".reasonix"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), opaquePNG(t, 8, 8), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".reasonix", "attachments")); err != nil {
		t.Fatal(err)
	}
	_, err := testService(t).PrepareBatch(t.Context(), []Source{{WorkspaceRoot: root, Path: ".reasonix/attachments/secret.png", Confine: ".reasonix/attachments"}})
	if err == nil {
		t.Fatal("accepted image outside workspace through parent symlink")
	}
}

// Done is first consulted by share after it has registered this waiter.
type joinedVariantContext struct {
	context.Context
	joined chan struct{}
	once   sync.Once
}

func (c *joinedVariantContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.joined) })
	return c.Context.Done()
}

func TestVariantWaitersCancelIndependently(t *testing.T) {
	cache := NewVariantCache(DefaultCacheBytes, 2)
	key := variantKey{digest: "independent"}
	started, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() {
		_, err := cache.share(ctx, key, func(work context.Context) (Variant, error) {
			close(started)
			<-release
			return Variant{Bytes: []byte("shared")}, work.Err()
		})
		first <- err
	}()
	<-started
	secondCtx := &joinedVariantContext{Context: t.Context(), joined: make(chan struct{})}
	second := make(chan error, 1)
	go func() {
		value, err := cache.share(secondCtx, key, func(context.Context) (Variant, error) {
			t.Error("second waiter started a second transform")
			return Variant{}, nil
		})
		if err == nil && string(value.Bytes) != "shared" {
			t.Error("wrong shared result")
		}
		second <- err
	}()
	<-secondCtx.joined
	cancel()
	if err := <-first; !Is(err, CodeCanceled) {
		t.Fatalf("first waiter = %v", err)
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatalf("second waiter canceled by first: %v", err)
	}
}

func TestCanceledVariantCannotCaptureSuccessor(t *testing.T) {
	cache := NewVariantCache(DefaultCacheBytes, 2)
	key := variantKey{digest: "test"}
	started, finish, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		defer close(returned)
		_, _ = cache.share(ctx, key, func(workCtx context.Context) (Variant, error) {
			close(started)
			<-finish
			return Variant{}, workCtx.Err()
		})
	}()
	<-started
	cancel()
	<-returned
	defer close(finish)
	result, err := cache.share(t.Context(), key, func(context.Context) (Variant, error) {
		return Variant{Bytes: []byte("successor")}, nil
	})
	if err != nil || string(result.Bytes) != "successor" {
		t.Fatalf("successor result: %q, %v", result.Bytes, err)
	}
}
