package control_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/permission"
)

const identityPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func newIdentityController(t *testing.T, workspace string) *control.Controller {
	t.Helper()
	c := control.New(control.Options{
		WorkspaceRoot: workspace,
		Sink:          event.Discard,
		Policy:        permission.New("allow", nil, nil, nil),
	})
	t.Cleanup(func() { c.Close() })
	return c
}

func TestIndependentAttachmentIdentityUsesBoundWorkspace(t *testing.T) {
	processDir := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	t.Chdir(processDir)
	raw, err := base64.StdEncoding.DecodeString(identityPNG)
	if err != nil {
		t.Fatal(err)
	}
	firstPath, err := control.SaveImageBytesInRoot(first, "image/png", raw)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes := append([]byte(nil), raw...)
	secondBytes[len(secondBytes)-1] ^= 1
	secondPath, err := control.SaveImageBytesInRoot(second, "image/png", secondBytes)
	if err != nil {
		t.Fatal(err)
	}
	one, err := control.ImageDataURLInRoot(first, firstPath)
	if err != nil {
		t.Fatal(err)
	}
	two, err := control.ImageDataURLInRoot(second, secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Fatal("same-named images in separate workspaces resolved to the same bytes")
	}
	if _, err := os.Stat(filepath.Join(processDir, ".reasonix")); !os.IsNotExist(err) {
		t.Fatal("attachment write used the process working directory")
	}
}

func TestIndependentAttachmentAdmissionKeepsDraftOnFailure(t *testing.T) {
	workspace := t.TempDir()
	c := newIdentityController(t, workspace)
	draft, err := c.StageImage(context.Background(), "shot.png", "image/png", "data:image/png;base64,"+identityPNG)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SubmitIdentified(control.SubmissionRequest{
		ID:    "sub-keep-draft",
		Input: "inspect @.reasonix/attachments/missing.png",
	})
	var failures control.ImageReferenceFailures
	if !errors.As(err, &failures) {
		t.Fatalf("submit = %v, want image reference failure", err)
	}
	got, raw, err := c.ReadDraftImage(context.Background(), draft.ID)
	if err != nil || got.ID != draft.ID || len(raw) == 0 {
		t.Fatalf("draft was released after a rejected turn: %v %+v", err, got)
	}
}

func TestIndependentAttachmentPermissionProfilesMatch(t *testing.T) {
	workspace := t.TempDir()
	raw, err := base64.StdEncoding.DecodeString(identityPNG)
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, mode := range []string{control.ToolApprovalWorkspaceWrite, control.ToolApprovalDangerFullAccess} {
		c := newIdentityController(t, workspace)
		c.SetToolApprovalMode(mode)
		draft, err := c.StageImage(context.Background(), "shot.png", "image/png", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(raw))
		if err != nil {
			t.Fatalf("mode %s stage: %v", mode, err)
		}
		_, body, err := c.ReadDraftImage(context.Background(), draft.ID)
		if err != nil {
			t.Fatalf("mode %s read: %v", mode, err)
		}
		if want == nil {
			want = body
		} else if !bytes.Equal(want, body) {
			t.Fatal("permission profiles produced different originals")
		}
	}
}
