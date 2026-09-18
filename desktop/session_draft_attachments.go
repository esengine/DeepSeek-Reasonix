package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/desktop/internal/draftstate"
	"reasonix/internal/control"
)

func draftAdmissionError(err error) error {
	stable := inboxBridgeError(err)
	var coded *inboxCodedError
	if errors.As(stable, &coded) {
		return stable
	}
	return fmt.Errorf("draft submission not admitted: %w", err)
}

func validateDraftAttachments(record draftstate.Draft) error {
	var content struct {
		Attachments []struct {
			Path string `json:"path"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(record.ContentJSON), &content); err != nil {
		return fmt.Errorf("decode session draft attachments: %w", err)
	}
	root := record.WorkspaceRoot
	if record.Scope != "project" {
		root = globalWorkspaceRoot()
	}
	base, err := workspaceBaseFromRoot(root)
	if err != nil {
		return err
	}
	for _, item := range content.Attachments {
		if err := control.ValidateAttachmentInRoot(base, item.Path); err != nil {
			if draftAttachmentLooksLikeImage(item.Path) {
				return control.ImageReferenceFailures{control.ImageReferenceFailureForPath(item.Path, err)}
			}
			return fmt.Errorf("attachment %q is unavailable: %w", filepath.Base(item.Path), err)
		}
	}
	return nil
}

func draftAttachmentLooksLikeImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".tif", ".tiff":
		return true
	default:
		return false
	}
}
