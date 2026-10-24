package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"reasonix/internal/base/fileutil"
)

type applyPostimageKey struct{}
type ApplyPostimageValidator func(context.Context, map[string]fileutil.ContentVersion) error

// WithApplyPostimageValidator checks the exact images that will be journaled.
// It runs under the apply lock, after filter/mode resolution and before any
// journal or source write. It does not grant write or recovery authority.
func WithApplyPostimageValidator(ctx context.Context, validate ApplyPostimageValidator) context.Context {
	return context.WithValue(ctx, applyPostimageKey{}, validate)
}

func validateApplyPostimage(ctx context.Context, journal *applyJournal) error {
	validate, _ := ctx.Value(applyPostimageKey{}).(ApplyPostimageValidator)
	if validate == nil {
		return nil
	}
	versions := make(map[string]fileutil.ContentVersion, len(journal.Files))
	for _, record := range journal.Files {
		version := fileutil.ContentVersion{Kind: "absent"}
		if record.After.Kind != "" {
			digest := sha256.Sum256(record.After.Data)
			version = fileutil.ContentVersion{Kind: record.After.Kind, Mode: uint32(imageMode(record.After.Mode)), Digest: hex.EncodeToString(digest[:])}
		}
		versions[record.Path] = version
	}
	return validate(ctx, versions)
}
