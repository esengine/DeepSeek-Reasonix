package attachment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/sessioncontent"
)

func TestPrepareAndCommitRoundTrip(t *testing.T) {
	svc := testService(t)
	raw := opaquePNG(t, 8, 8)
	prepared, err := svc.PrepareBatch(t.Context(), []Source{{DisplayName: "shot.png", Bytes: raw, DeclaredMIME: "image/png"}})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := svc.CommitBatch(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Width != 8 || refs[0].MIME() != "image/png" {
		t.Fatalf("refs = %+v", refs)
	}
	got, err := svc.ReadVerified(t.Context(), refs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("persisted bytes diverged from the original")
	}
	sum := sha256.Sum256(raw)
	if refs[0].Content.Digest != hex.EncodeToString(sum[:]) {
		t.Fatal("object identity is not the original digest")
	}
}

func TestPrepareBatchRejectsPartialFailure(t *testing.T) {
	svc := testService(t)
	_, err := svc.PrepareBatch(t.Context(), []Source{
		{DisplayName: "ok.png", Bytes: opaquePNG(t, 2, 2)},
		{DisplayName: "bad.bin", Bytes: []byte("not-an-image")},
	})
	if !Is(err, CodeUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareBatchRejectsDeclaredMIMEMismatch(t *testing.T) {
	svc := testService(t)
	_, err := svc.PrepareBatch(t.Context(), []Source{{
		DisplayName: "spoof.png", Bytes: opaquePNG(t, 2, 2), DeclaredMIME: "image/jpeg",
	}})
	if !Is(err, CodeUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareBatchUsesWorkspaceRootNotProcessCWD(t *testing.T) {
	svc := testService(t)
	cwd := t.TempDir()
	workspace := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.WriteFile(filepath.Join(cwd, "same.png"), opaquePNG(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	want := opaquePNG(t, 4, 4)
	if err := os.WriteFile(filepath.Join(workspace, "same.png"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.PrepareBatch(t.Context(), []Source{{Path: "same.png", WorkspaceRoot: workspace}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prepared.Items[0].Bytes, want) || prepared.Items[0].Width != 4 {
		t.Fatal("prepared the process-directory image")
	}
}

func TestPrepareBatchRejectsSymlinkAndChangedFile(t *testing.T) {
	svc := testService(t)
	root := t.TempDir()
	target := filepath.Join(root, "real.png")
	if err := os.WriteFile(target, opaquePNG(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareBatch(t.Context(), []Source{{Path: link}}); !Is(err, CodeUnsafe) {
		t.Fatalf("symlink err = %v", err)
	}

	path := filepath.Join(root, "changing.png")
	if err := os.WriteFile(path, opaquePNG(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	src := Source{Path: path}
	raw, err := src.loadBytes(DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("expected bytes")
	}
}

func TestCommitDoesNotReturnPartialRefs(t *testing.T) {
	svc := NewService(nil, nil)
	_, err := svc.CommitBatch(t.Context(), PreparedImages{Items: []PreparedImage{{DisplayName: "a", Bytes: opaquePNG(t, 2, 2)}}})
	if err == nil {
		t.Fatal("expected store failure")
	}
}

func TestDraftsCannotBeGuessedByDigest(t *testing.T) {
	svc := testService(t)
	prepared, err := svc.PrepareBatch(t.Context(), []Source{{Bytes: opaquePNG(t, 2, 2)}})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := svc.CommitBatch(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	draft := svc.Drafts().Issue("session-a", refs[0])
	if _, ok := svc.Drafts().Lookup("session-b", draft.ID); ok {
		t.Fatal("draft leaked across scopes")
	}
	if _, ok := svc.Drafts().Lookup("session-a", refs[0].Content.Digest); ok {
		t.Fatal("digest acted as a draft credential")
	}
	got, err := svc.Drafts().Resolve("session-a", []string{draft.ID})
	if err != nil || got[0].Content.Digest != refs[0].Content.Digest {
		t.Fatalf("resolve = %v %v", got, err)
	}
	svc.Drafts().Release("session-a", draft.ID)
	if _, err := svc.Drafts().Resolve("session-a", []string{draft.ID}); !Is(err, CodeMissing) {
		t.Fatalf("released draft err = %v", err)
	}
}

func TestPrepareVariantIsDeterministicAndRebuildsAfterEviction(t *testing.T) {
	svc := testService(t)
	raw := opaquePNG(t, 2000, 100)
	prepared, err := svc.PrepareBatch(t.Context(), []Source{{Bytes: raw}})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := svc.CommitBatch(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.PrepareVariant(t.Context(), refs[0], VariantPolicyV1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Width != VariantMaxDim || first.MIME != "image/png" {
		t.Fatalf("variant = %+v", first)
	}
	second, err := svc.PrepareVariant(t.Context(), refs[0], VariantPolicyV1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes, second.Bytes) || first.Digest != second.Digest {
		t.Fatal("variant bytes were not stable")
	}
	svc.Cache().remove(variantKey{digest: refs[0].Content.Digest, version: VariantPolicyV1, width: first.Width, height: first.Height, format: "png"})
	rebuilt, err := svc.PrepareVariant(t.Context(), refs[0], VariantPolicyV1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes, rebuilt.Bytes) {
		t.Fatal("rebuilt variant diverged")
	}
}

func TestPrepareVariantCacheHitProbesAndReplacementRevalidatesOriginal(t *testing.T) {
	svc := testService(t)
	raw := opaquePNG(t, 2000, 100)
	prepared, err := svc.PrepareBatch(t.Context(), []Source{{DisplayName: "original.png", Bytes: raw}})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := svc.CommitBatch(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	ref := refs[0]
	if _, err = svc.PrepareVariant(t.Context(), ref, VariantPolicyV1); err != nil {
		t.Fatal(err)
	}
	objectPath := filepath.Join(svc.Store().Root(), "objects", ref.Content.Digest[:2], ref.Content.Digest[2:4], ref.Content.Digest)
	backup := objectPath + ".verified"
	if err = os.Rename(objectPath, backup); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PrepareVariant(t.Context(), ref, VariantPolicyV1); err == nil {
		t.Fatal("cache hit bypassed the bounded original-object probe")
	}
	corrupt := bytes.Repeat([]byte{'x'}, len(raw))
	if err = os.WriteFile(objectPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PrepareVariant(t.Context(), ref, VariantPolicyV1); err == nil {
		t.Fatal("replacement object reused a variant without full validation")
	}
	if err = os.Remove(objectPath); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(backup, objectPath); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PrepareVariant(t.Context(), ref, VariantPolicyV1); err != nil {
		t.Fatalf("restored original did not rebuild the variant: %v", err)
	}
}

func TestVariantCancelIsolatesWaiters(t *testing.T) {
	cache := NewVariantCache(DefaultCacheBytes, 1)
	raw := opaquePNG(t, 1800, 1800)
	ref := AttachmentRef{Version: RefVersion, Content: sessioncontent.Ref{Digest: strings.Repeat("a", 64), Bytes: int64(len(raw)), MediaType: "image/png"}, Width: 1800, Height: 1800}
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Add(2)
	var canceled, succeeded error
	go func() {
		defer wg.Done()
		_, canceled = cache.Prepare(ctx, ref, raw, VariantPolicyV1)
	}()
	go func() {
		defer wg.Done()
		_, succeeded = cache.Prepare(t.Context(), ref, raw, VariantPolicyV1)
	}()
	cancel()
	wg.Wait()
	if canceled == nil || !Is(canceled, CodeCanceled) {
		t.Fatalf("canceled waiter err = %v", canceled)
	}
	if succeeded != nil {
		t.Fatalf("remaining waiter err = %v", succeeded)
	}
}

func TestCollectJSONRefsFindsNestedAttachments(t *testing.T) {
	ref := sessioncontent.Ref{Digest: strings.Repeat("ab", 32), Bytes: 12, IndexDigest: strings.Repeat("cd", 32)}
	payload := jsonMarshal(map[string]any{
		"message": map[string]any{
			"image_inputs": []any{map[string]any{
				"kind": "attachment",
				"attachment": map[string]any{
					"v":       1,
					"content": map[string]any{"digest": ref.Digest, "bytes": ref.Bytes, "indexDigest": ref.IndexDigest},
				},
			}},
		},
	})
	got := CollectJSONRefs(payload)
	if len(got) != 1 || got[0].Digest != ref.Digest || got[0].Bytes != ref.Bytes {
		t.Fatalf("got = %+v", got)
	}
}

func TestNormalizeDisplayNameStripsPathAndControls(t *testing.T) {
	if got := NormalizeDisplayName("../secret/\x00shot.png"); got != "shot.png" {
		t.Fatalf("got %q", got)
	}
}

func TestViewImagePolicyRejectsOversize(t *testing.T) {
	svc := testService(t).WithPolicy(Policy{MaxBytes: 32, MaxPixels: 4, MaxCount: 1, MaxBatchBytes: 32})
	_, err := svc.PrepareBatch(t.Context(), []Source{{Bytes: opaquePNG(t, 8, 8)}})
	if !Is(err, CodeSize) {
		t.Fatalf("err = %v", err)
	}
}

func testService(t *testing.T) *Service {
	t.Helper()
	return NewService(sessioncontent.New(t.TempDir()), NewVariantCache(8<<20, 2))
}

func opaquePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 20, G: 40, B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jsonMarshal(v any) []byte {
	buf, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return buf
}

func TestTinyPNGDetect(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	if DetectMIME(raw) != "image/png" {
		t.Fatal(DetectMIME(raw))
	}
}
