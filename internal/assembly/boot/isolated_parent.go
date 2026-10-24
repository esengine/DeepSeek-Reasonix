package boot

import (
	"context"

	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
)

type isolatedParent struct {
	store     *isolation.Store
	entry     *isolation.Entry
	source    string
	authorize isolation.SourceAuthorizer
}

func prepareIsolatedEntry(ctx context.Context, parent *isolatedParent, store *isolation.Store, source string, origin *delegation.EnvironmentSource, owner string) (*isolation.Entry, error) {
	resumeID := ""
	if origin != nil {
		resumeID = origin.ID
		if origin.Kind == delegation.EnvironmentCopy {
			copySource := isolation.CopySource{ID: origin.ID, Generation: origin.Generation, Owner: origin.Owner, Workspace: origin.WorkspaceRoot}
			if parent != nil {
				return parent.store.CopyChild(ctx, parent.entry, store, copySource, owner)
			}
			return store.CopyOwned(ctx, copySource, owner)
		}
	}
	if parent != nil {
		if resumeID == "" {
			return parent.store.BeginChild(ctx, parent.entry, store, owner)
		}
		return parent.store.ResumeChild(ctx, parent.entry, store, resumeID, owner)
	}
	if resumeID == "" {
		return store.BeginOwned(ctx, source, owner)
	}
	return store.Resume(ctx, resumeID, owner)
}
