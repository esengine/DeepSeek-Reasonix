package agent

// SessionPreviewCached returns the preview line and user-turn count for one
// session, preferring the branch-meta sidecar when its counts are certified
// fresh and bound to the current transcript revision: no transcript decode.
// ok=false means the sidecar is absent or stale — the caller should fall
// back to SessionPreview's full decode.
func SessionPreviewCached(path string) (preview string, turns int, ok bool) {
	meta, loaded, err := LoadBranchMeta(path)
	if err != nil || !loaded {
		return "", 0, false
	}
	if !sessionListingProjectionFresh(meta.SchemaVersion, meta.Turns, meta.Revision, meta.ListingRevision, meta.ContentDigest, meta.ListingContentDigest) {
		return "", 0, false
	}
	return meta.Preview, meta.Turns, true
}
