package session

import "time"

// Source identifies the legacy artifact a canonical session was migrated from.
// CreatedAt and UpdatedAt are the source session's own times, preserved so a
// migrated target orders by when the conversation happened rather than when it
// was imported. They are zero on targets published before the times were
// recorded; RepairMigratedActivity backfills them from the legacy evidence.
type Source struct {
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	SHA256       string    `json:"sha256"`
	Version      string    `json:"version,omitempty"`
	LegacyHeadID string    `json:"legacyHeadId,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}
