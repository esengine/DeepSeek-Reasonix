package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/attachment"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/store"
)

const attachmentReadPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestReadSessionAttachmentAuthorizesDigestOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "attach-read"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(attachmentReadPNG)
	if err != nil {
		t.Fatal(err)
	}
	content := contentStoreForSessionDir(filepath.Join(root, runtime.Ref().SessionID))
	ref, err := content.Put(t.Context(), bytes.NewReader(raw), sessioncontent.Metadata{MediaType: "image/png", Name: "shot.png"})
	if err != nil {
		t.Fatal(err)
	}
	msg := provider.Message{
		ID: "user-1", Role: provider.RoleUser, Content: "see this",
		ImageInputs: []attachment.ImageInput{{
			Kind: attachment.KindAttachment,
			Attachment: &attachment.AttachmentRef{
				Version: attachment.RefVersion, Content: ref, Width: 1, Height: 1, DisplayName: "shot.png",
			},
		}},
	}
	payload, err := json.Marshal(map[string]any{"message": msg})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "user-1", []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = historyPageReady(t, service.Query(), runtime.Ref(), "", 8)

	got, total, err := service.Query().ReadSessionAttachment(t.Context(), runtime.Ref(), ref.Digest, 0, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len(raw)) || !bytes.Equal(got, raw) {
		t.Fatalf("read digest-only attachment total=%d len=%d", total, len(got))
	}
	sum := sha256.Sum256(raw)
	if ref.Digest != hex.EncodeToString(sum[:]) {
		t.Fatal("fixture digest is not the original SHA-256")
	}
	foreign := strings.Repeat("0", 64)
	if _, _, err := service.Query().ReadSessionAttachment(t.Context(), runtime.Ref(), foreign, 0, 1); err == nil {
		t.Fatal("foreign digest was authorized")
	}
	if _, _, err := service.Query().ReadSessionAttachment(t.Context(), runtime.Ref(), ref.Digest, 0, (1<<20)+1); err == nil {
		t.Fatal("oversized attachment range was accepted")
	}
}

func TestReadSessionAttachmentAuthorizesInboxRefs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "attach-inbox"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(attachmentReadPNG)
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(root, runtime.Ref().SessionID)
	content := contentStoreForSessionDir(sessionDir)
	ref, err := content.Put(t.Context(), bytes.NewReader(raw), sessioncontent.Metadata{MediaType: "image/png", Name: "queued.png"})
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := sessioninbox.Open(sessionDir, sessioninbox.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inbox.Close() })
	if _, err := inbox.Enqueue(sessioninbox.EnqueueRequest{Envelope: sessioninbox.PromptEnvelope{
		SubmitText: "queued image",
		ImageInputs: []attachment.ImageInput{{
			Kind: attachment.KindAttachment,
			Attachment: &attachment.AttachmentRef{
				Version: attachment.RefVersion, Content: ref, Width: 1, Height: 1, DisplayName: "queued.png",
			},
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	got, total, err := service.Query().ReadSessionAttachment(t.Context(), runtime.Ref(), ref.Digest, 0, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len(raw)) || !bytes.Equal(got, raw) {
		t.Fatalf("inbox attachment total=%d len=%d", total, len(got))
	}
	if _, err := os.Stat(store.SessionInboxDir(sessionDir)); err != nil {
		t.Fatal(err)
	}
}

func TestExportIncludesMessageAttachmentClosure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "export-images"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(attachmentReadPNG)
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(root, runtime.Ref().SessionID)
	content := contentStoreForSessionDir(sessionDir)
	ref, err := content.Put(t.Context(), bytes.NewReader(raw), sessioncontent.Metadata{MediaType: "image/png", Name: "shot.png"})
	if err != nil {
		t.Fatal(err)
	}
	message := provider.Message{ID: "user-image", Role: provider.RoleUser, Content: "see this", ImageInputs: []attachment.ImageInput{{
		Kind: attachment.KindAttachment,
		Attachment: &attachment.AttachmentRef{
			Version: attachment.RefVersion, Content: ref, Width: 1, Height: 1, DisplayName: "shot.png",
		},
	}}}
	payload, err := json.Marshal(map[string]any{"message": message})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.Session().AppendBatch(t.Context(), "user-image", []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}

	exported := filepath.Join(t.TempDir(), "exported-session")
	if err := service.Export(t.Context(), runtime.Ref(), exported); err != nil {
		t.Fatal(err)
	}
	copied, err := sessioncontent.New(filepath.Join(exported, ".content-v1")).ReadRange(t.Context(), ref, 0, ref.Bytes)
	if err != nil || !bytes.Equal(copied, raw) {
		t.Fatalf("exported message attachment missing: %v", err)
	}
}

func TestExportRefusesMissingAttachmentObject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := NewService("local", NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), CreateOptions{SessionID: "export-missing"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(attachmentReadPNG)
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(root, runtime.Ref().SessionID)
	content := contentStoreForSessionDir(sessionDir)
	ref, err := content.Put(t.Context(), bytes.NewReader(raw), sessioncontent.Metadata{MediaType: "image/png", Name: "shot.png"})
	if err != nil {
		t.Fatal(err)
	}
	msg := provider.Message{
		ID: "user-1", Role: provider.RoleUser, Content: "see this",
		ImageInputs: []attachment.ImageInput{{
			Kind: attachment.KindAttachment,
			Attachment: &attachment.AttachmentRef{
				Version: attachment.RefVersion, Content: ref, Width: 1, Height: 1, DisplayName: "shot.png",
			},
		}},
	}
	payload, err := json.Marshal(map[string]any{"message": msg})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "user-1", []Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(content.Root()); err != nil {
		t.Fatal(err)
	}
	if err := service.Export(t.Context(), runtime.Ref(), filepath.Join(t.TempDir(), "broken-export")); err == nil {
		t.Fatal("export accepted a missing attachment object")
	}
}
