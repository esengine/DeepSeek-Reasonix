// Package protocol defines the frozen Reasonix Remote V1 wire contract.
package protocol

// Opaque identities deliberately use distinct Go types. Their serialized form
// is a non-empty string, but callers must not derive or parse business meaning
// from the bytes.
type (
	WorkspaceID      string
	SessionID        string
	RequestID        string
	HostEpoch        string
	RuntimeEpoch     string
	TurnID           string
	OperationID      string
	PromptID         string
	CheckpointID     string
	SubscriptionID   string
	SnapshotID       string
	ContentRef       string
	Cursor           string
	LeaseID          string
	ClientInstanceID string
	DirectoryRef     string
	TopicID          string
	CatalogRevision  string
	MemoryID         string
	DocumentID       string
	SuggestionID     string
	SkillID          string
	ResearchTaskID   string
	CriterionID      string
	JobID            string
	QuestionID       string
	ModelRef         string
)
