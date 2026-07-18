package protocol

type SessionCatalogParams struct{ RuntimeQuery }

type CommandCatalogItem struct {
	Name        string `json:"name" validate:"nonempty"`
	Description string `json:"description,omitempty"`
}

type MCPServerCatalogItem struct {
	Name      string `json:"name" validate:"nonempty"`
	Available bool   `json:"available"`
	ToolCount int    `json:"toolCount" validate:"min=0"`
}

type SkillCatalogItem struct {
	ID          SkillID `json:"id"`
	Name        string  `json:"name" validate:"nonempty"`
	Description string  `json:"description,omitempty"`
	Scope       string  `json:"scope" validate:"nonempty"`
}

type PluginCatalogItem struct {
	ID      string `json:"id" validate:"nonempty"`
	Name    string `json:"name" validate:"nonempty"`
	Enabled bool   `json:"enabled"`
}

type SessionCatalogResult struct {
	Revision   CatalogRevision        `json:"revision"`
	Commands   []CommandCatalogItem   `json:"commands"`
	MCPServers []MCPServerCatalogItem `json:"mcpServers"`
	Skills     []SkillCatalogItem     `json:"skills"`
	Plugins    []PluginCatalogItem    `json:"plugins"`
}

type ComposerSlashArgsParams struct {
	RuntimeQuery
	Input string `json:"input"`
}

type SlashArgItem struct {
	Label   string `json:"label" validate:"nonempty"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint,omitempty"`
	Descend bool   `json:"descend"`
}

type ComposerSlashArgsResult struct {
	Items []SlashArgItem `json:"items"`
	From  int            `json:"from" validate:"min=0"`
}

type ComposerHistoryParams struct {
	ExpectedHostEpoch HostEpoch   `json:"expectedHostEpoch"`
	WorkspaceID       WorkspaceID `json:"workspaceId"`
	Cursor            Cursor      `json:"cursor,omitempty"`
	Limit             *int        `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type PromptHistoryEntry struct {
	Text   string        `json:"text" validate:"nonempty"`
	AtMs   int64         `json:"at" validate:"min=0"`
	Target RuntimeTarget `json:"target"`
	Turn   int           `json:"turn" validate:"min=0"`
}

type ComposerHistoryResult struct {
	Entries    []PromptHistoryEntry `json:"entries"`
	HasMore    bool                 `json:"hasMore"`
	NextCursor Cursor               `json:"nextCursor,omitempty"`
}

type MemoryGetParams struct{ RuntimeQuery }

type MemoryDocument struct {
	DocumentID  DocumentID `json:"documentId"`
	Scope       string     `json:"scope" validate:"nonempty"`
	Body        *string    `json:"body"`
	DisplayPath string     `json:"displayPath" validate:"nonempty"`
}

type MemoryFact struct {
	MemoryID    MemoryID `json:"memoryId"`
	Name        string   `json:"name" validate:"nonempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description"`
	Type        string   `json:"type" validate:"nonempty"`
	Body        *string  `json:"body"`
}

type MemoryArchive struct {
	MemoryFact
	ArchivedAt string `json:"archivedAt,omitempty"`
}

type MemoryScope struct {
	Scope       string `json:"scope" validate:"nonempty"`
	DisplayPath string `json:"displayPath" validate:"nonempty"`
}

type MemoryGetResult struct {
	Revision  CatalogRevision  `json:"revision"`
	Available bool             `json:"available"`
	Documents []MemoryDocument `json:"documents"`
	Facts     []MemoryFact     `json:"facts"`
	Archives  []MemoryArchive  `json:"archives"`
	Scopes    []MemoryScope    `json:"scopes"`
}

type MemorySuggestionsParams struct{ RuntimeQuery }

type MemorySuggestion struct {
	SuggestionID SuggestionID `json:"suggestionId"`
	Name         string       `json:"name" validate:"nonempty"`
	Title        string       `json:"title" validate:"nonempty"`
	Description  string       `json:"description"`
	Type         string       `json:"type" validate:"nonempty"`
	Body         *string      `json:"body"`
	Reason       string       `json:"reason"`
	Evidence     []string     `json:"evidence"`
}

type SkillSuggestion struct {
	SuggestionID SuggestionID `json:"suggestionId"`
	Name         string       `json:"name" validate:"nonempty"`
	Description  string       `json:"description"`
	Scope        string       `json:"scope" validate:"nonempty"`
	Body         *string      `json:"body"`
	Reason       string       `json:"reason"`
	Evidence     []string     `json:"evidence"`
}

type MemorySuggestionsResult struct {
	Revision  CatalogRevision    `json:"revision"`
	Available bool               `json:"available"`
	Memories  []MemorySuggestion `json:"memories"`
	Skills    []SkillSuggestion  `json:"skills"`
}

type MemoryRememberParams struct {
	SessionMutation
	Scope string `json:"scope" validate:"nonempty"`
	Note  string `json:"note" validate:"nonempty"`
}

type MemoryRememberResult struct {
	MemoryID    MemoryID `json:"memoryId"`
	DisplayPath string   `json:"displayPath" validate:"nonempty"`
}

type MemoryForgetParams struct {
	SessionMutation
	MemoryID MemoryID `json:"memoryId"`
}

type MemoryForgetResult struct {
	Forgotten bool `json:"forgotten" validate:"true"`
}

type MemoryDocumentSaveParams struct {
	SessionMutation
	DocumentID DocumentID `json:"documentId"`
	Body       string     `json:"body"`
}

type MemoryDocumentSaveResult struct {
	DocumentID DocumentID `json:"documentId"`
	Saved      bool       `json:"saved" validate:"true"`
}

type MemorySuggestionAcceptParams struct {
	SessionMutation
	SuggestionID     SuggestionID    `json:"suggestionId"`
	ExpectedRevision CatalogRevision `json:"expectedRevision"`
}

type MemorySuggestionAcceptResult struct {
	MemoryID MemoryID `json:"memoryId"`
}

type SkillSuggestionAcceptParams struct {
	SessionMutation
	SuggestionID     SuggestionID    `json:"suggestionId"`
	ExpectedRevision CatalogRevision `json:"expectedRevision"`
}

type SkillSuggestionAcceptResult struct {
	SkillID SkillID `json:"skillId"`
}

type ResearchStatusParams struct{ RuntimeQuery }

type ResearchCriterion struct {
	CriterionID   CriterionID `json:"criterionId"`
	Description   string      `json:"description" validate:"nonempty"`
	Required      bool        `json:"required"`
	EvidenceCount int         `json:"evidenceCount" validate:"min=0"`
	Status        string      `json:"status" validate:"nonempty"`
}

type ResearchTask struct {
	TaskID             ResearchTaskID      `json:"taskId"`
	Goal               *string             `json:"goal"`
	Status             string              `json:"status" validate:"nonempty"`
	Iteration          int                 `json:"iteration" validate:"min=0"`
	CurrentDirection   *string             `json:"currentDirection"`
	StaleCount         int                 `json:"staleCount" validate:"min=0"`
	PivotCount         int                 `json:"pivotCount" validate:"min=0"`
	PivotRequired      bool                `json:"pivotRequired"`
	LastHeartbeatAt    string              `json:"lastHeartbeatAt,omitempty"`
	FindingCount       int                 `json:"findingCount" validate:"min=0"`
	OpenCriteria       []ResearchCriterion `json:"openCriteria"`
	Blocker            *string             `json:"blocker,omitempty"`
	DisplayPath        string              `json:"displayPath,omitempty"`
	NextRequiredAction *string             `json:"nextRequiredAction,omitempty"`
}

type ResearchStatusResult struct {
	Available bool          `json:"available"`
	Task      *ResearchTask `json:"task,omitempty"`
}

type ResearchListParams struct {
	RuntimeQuery
	Cursor Cursor `json:"cursor,omitempty"`
	Limit  *int   `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type ResearchListResult struct {
	Items      []ResearchTask `json:"items"`
	HasMore    bool           `json:"hasMore"`
	NextCursor Cursor         `json:"nextCursor,omitempty"`
}

type ResearchFindingsParams struct {
	RuntimeQuery
	TaskID ResearchTaskID `json:"taskId"`
	Cursor Cursor         `json:"cursor,omitempty"`
	Limit  *int           `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type ResearchFinding struct {
	ID        string   `json:"id" validate:"nonempty"`
	Kind      string   `json:"kind" validate:"nonempty"`
	Summary   *string  `json:"summary"`
	Source    string   `json:"source" validate:"nonempty"`
	Command   string   `json:"command,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Accepted  bool     `json:"accepted"`
	CreatedAt string   `json:"createdAt" validate:"nonempty"`
}

type ResearchFindingsResult struct {
	Items      []ResearchFinding `json:"items"`
	HasMore    bool              `json:"hasMore"`
	NextCursor Cursor            `json:"nextCursor,omitempty"`
}

type ResearchEvidence struct {
	ID       string   `json:"id" validate:"nonempty"`
	Kind     string   `json:"kind" validate:"nonempty"`
	Summary  string   `json:"summary" validate:"nonempty"`
	Source   string   `json:"source" validate:"nonempty"`
	Command  string   `json:"command,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Accepted bool     `json:"accepted"`
}

type ResearchEvidenceRecordParams struct {
	SessionMutation
	TaskID      ResearchTaskID   `json:"taskId"`
	CriterionID CriterionID      `json:"criterionId"`
	Evidence    ResearchEvidence `json:"evidence"`
}

type ResearchEvidenceRecordResult struct {
	Recorded bool `json:"recorded" validate:"true"`
}
