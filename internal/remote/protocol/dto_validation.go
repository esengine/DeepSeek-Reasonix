package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"reflect"

	"reasonix/internal/eventwire"
)

func (l LeaseInfo) Validate() error {
	if l.TTLMillis != LeaseTTLMillis || l.PingIntervalMs != LeasePingIntervalMillis {
		return validationError("lease timing does not match the frozen 30s/10s contract")
	}
	return nil
}

func (r PingResult) Validate() error {
	if r.LeaseTTL != LeaseTTLMillis {
		return validationError("leaseTtlMs must be 30000")
	}
	return nil
}

func (f ExternalizedField) Validate() error {
	if f.TotalBytes > ContentRefObjectBytes {
		return validationError("externalized content exceeds the 8 MiB object limit")
	}
	if f.Truncated {
		if f.OriginalBytes == nil || *f.OriginalBytes <= f.TotalBytes || f.TruncationReason == "" {
			return validationError("truncated externalized content requires larger originalBytes and a reason")
		}
	} else if f.OriginalBytes != nil || f.TruncationReason != "" {
		return validationError("non-truncated externalized content forbids truncation metadata")
	}
	return nil
}

func (p HistoryPage) Validate() error {
	if err := validateExternalizedPointers(reflect.TypeOf(p), p.Externalized); err != nil {
		return err
	}
	if p.StartTurn > p.EndTurn || p.EndTurn > p.TotalTurns || p.ActualTurns != p.EndTurn-p.StartTurn {
		return validationError("history turn range is inconsistent")
	}
	if p.HasOlder != (p.StartTurn > 0) {
		return validationError("hasOlder must match the page start")
	}
	return validatePageCursor(p.HasOlder, p.NextCursor)
}

func (r SessionContentResult) Validate() error {
	decoded, err := base64.StdEncoding.DecodeString(r.DataBase64)
	if err != nil {
		return validationError("dataBase64 is invalid")
	}
	if len(decoded) > ContentRefChunkBytes || r.TotalBytes > ContentRefObjectBytes || r.Offset > r.TotalBytes {
		return validationError("content chunk exceeds its byte bounds")
	}
	end := r.Offset + int64(len(decoded))
	if end > r.TotalBytes {
		return validationError("content chunk extends past totalBytes")
	}
	if end < r.TotalBytes {
		if r.NextOffset == nil || *r.NextOffset != end || *r.NextOffset <= r.Offset {
			return validationError("non-final content chunk requires the exact increasing nextOffset")
		}
	} else if r.NextOffset != nil {
		return validationError("final content chunk must omit nextOffset")
	}
	return nil
}

func (e SessionEvent) Validate() error {
	if e.Seq == 0 {
		return validationError("session event seq must start at 1")
	}
	if e.TurnID != "" && e.OperationID != "" {
		return validationError("turnId and operationId are mutually exclusive")
	}
	if !contains(eventwire.KindNames(), e.Event.Kind) {
		return validationError("session event kind is not registered by eventwire")
	}
	return validateExternalizedPointers(reflect.TypeOf(e), e.Externalized)
}

func (s SessionSnapshot) Validate() error {
	if s.History.SnapshotID != s.SnapshotID {
		return validationError("snapshot history must carry the owning snapshotId")
	}
	for _, liveEvent := range s.Runtime.LiveEvents {
		if !contains(eventwire.KindNames(), liveEvent.Kind) {
			return validationError("snapshot live event kind is not registered by eventwire")
		}
	}
	return validateExternalizedPointers(reflect.TypeOf(s), s.Externalized)
}

func (r SessionResyncRequired) Validate() error {
	switch r.Reason {
	case ResyncQueueOverflow, ResyncStateChanged:
		if r.ReplacementTarget != nil || r.ReplacementRuntimeEpoch != "" {
			return validationError("resync reason forbids replacement identity")
		}
	case ResyncRuntimeReplaced:
		if r.ReplacementTarget != nil || r.ReplacementRuntimeEpoch == "" {
			return validationError("runtime_replaced requires only replacementRuntimeEpoch")
		}
	case ResyncTargetReplaced:
		if r.ReplacementTarget == nil || r.ReplacementRuntimeEpoch == "" {
			return validationError("target_replaced requires replacement target and runtime epoch")
		}
	}
	return nil
}

func (c CatalogChanged) Validate() error {
	if len(c.Kinds) == 0 {
		return validationError("catalog change requires at least one kind")
	}
	seen := make(map[CatalogKind]bool, len(c.Kinds))
	for _, kind := range c.Kinds {
		if seen[kind] {
			return validationError("catalog change kinds must be unique")
		}
		seen[kind] = true
	}
	if c.Scope == CatalogWorkspace && len(c.AffectedWorkspaceIDs) == 0 {
		return validationError("workspace catalog change requires affectedWorkspaceIds")
	}
	if c.Scope == CatalogHost && len(c.AffectedWorkspaceIDs) != 0 {
		return validationError("host catalog change forbids affectedWorkspaceIds")
	}
	seenWorkspaces := make(map[WorkspaceID]bool, len(c.AffectedWorkspaceIDs))
	for _, workspaceID := range c.AffectedWorkspaceIDs {
		if seenWorkspaces[workspaceID] {
			return validationError("affectedWorkspaceIds must be unique")
		}
		seenWorkspaces[workspaceID] = true
	}
	return nil
}

func (r WorkspaceBrowseResult) Validate() error  { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r WorkspaceListResult) Validate() error    { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r SessionListResult) Validate() error      { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r TopicListResult) Validate() error        { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r SessionTrashListResult) Validate() error { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r ComposerHistoryResult) Validate() error  { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r ResearchListResult) Validate() error     { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r ResearchFindingsResult) Validate() error { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r FileListResult) Validate() error         { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r WorkspaceChangesResult) Validate() error { return validatePageCursor(r.HasMore, r.NextCursor) }
func (r JobListResult) Validate() error          { return validatePageCursor(r.HasMore, r.NextCursor) }

func (p ResearchListParams) Validate() error     { return validateResearchCursorShape(p.Cursor) }
func (p ResearchFindingsParams) Validate() error { return validateResearchCursorShape(p.Cursor) }

type researchCursorWireShape struct {
	Version     int    `json:"v"`
	Target      string `json:"t"`
	Incarnation string `json:"i"`
	Method      string `json:"m"`
	TaskID      string `json:"k,omitempty"`
	Revision    string `json:"r"`
	Offset      int    `json:"o"`
}

func validateResearchCursorShape(cursor Cursor) error {
	if cursor == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(string(cursor))
	if err != nil || len(raw) <= sha256.Size || base64.RawURLEncoding.EncodeToString(raw) != string(cursor) {
		return validationError("cursor has invalid Research cursor encoding")
	}
	var payload researchCursorWireShape
	if err := json.Unmarshal(raw[:len(raw)-sha256.Size], &payload); err != nil || payload.Version != 1 ||
		payload.Target == "" || payload.Incarnation == "" || payload.Method == "" || payload.Revision == "" || payload.Offset < 0 {
		return validationError("cursor has invalid Research cursor shape")
	}
	return nil
}

func (r FileSearchResult) Validate() error {
	if r.ReturnedItems != len(r.Entries) {
		return validationError("returnedItems must match entries length")
	}
	if r.TotalItems != nil && *r.TotalItems < r.ReturnedItems {
		return validationError("totalItems is smaller than returnedItems")
	}
	if r.Truncated != (r.TruncationReason != "") {
		return validationError("search truncation reason must be present exactly when truncated")
	}
	return nil
}

func (r GitHistoryResult) Validate() error {
	if r.ReturnedItems != len(r.Commits) {
		return validationError("returnedItems must match commits length")
	}
	if r.Truncated != (r.TruncationReason == GitHistoryLimit) {
		return validationError("Git history truncation must use history_limit")
	}
	return nil
}

func validatePageCursor(hasMore bool, cursor Cursor) error {
	if hasMore && cursor == "" {
		return validationError("hasMore requires nextCursor")
	}
	if !hasMore && cursor != "" {
		return validationError("nextCursor requires hasMore")
	}
	return nil
}
