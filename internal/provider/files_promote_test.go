package provider

import (
	"errors"
	"testing"
)

func TestPromoteDataURLsToFilesAllOrNothing(t *testing.T) {
	msgs := []Message{{
		ID: "u1", Role: RoleUser,
		Images: []string{"data:image/png;base64,AA==", "file-api-abcd1234"},
	}}
	got := PromoteDataURLsToFiles(msgs, func(string, []byte) (string, error) {
		return "file-api-newimage01", nil
	})
	if got[0].Images[0] != "file-api-newimage01" || got[0].Images[1] != "file-api-abcd1234" {
		t.Fatalf("promoted = %v", got[0].Images)
	}
	failed := PromoteDataURLsToFiles(msgs, func(string, []byte) (string, error) {
		return "", errors.New("quota")
	})
	if failed[0].Images[0] != msgs[0].Images[0] {
		t.Fatal("a failed upload must leave the original data URL")
	}
}
