package runtimeapi

type MemoryInput struct {
	Session SessionRef `json:"session"`
}

type MemoryDocument struct {
	DocumentID  DocumentID `json:"documentId"`
	Scope       string     `json:"scope"`
	Body        *string    `json:"body"`
	DisplayPath string     `json:"displayPath"`
}

type MemoryFact struct {
	MemoryID    MemoryID `json:"memoryId"`
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Body        *string  `json:"body"`
}

type MemoryArchive struct {
	MemoryFact
	ArchivedAt string `json:"archivedAt,omitempty"`
}

type MemoryScope struct {
	Scope       string `json:"scope"`
	DisplayPath string `json:"displayPath"`
}

type MemoryView struct {
	Revision  CatalogRevision  `json:"revision"`
	Available bool             `json:"available"`
	Documents []MemoryDocument `json:"documents"`
	Facts     []MemoryFact     `json:"facts"`
	Archives  []MemoryArchive  `json:"archives"`
	Scopes    []MemoryScope    `json:"scopes"`
}

type MemorySuggestion struct {
	SuggestionID SuggestionID `json:"suggestionId"`
	Name         string       `json:"name"`
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	Type         string       `json:"type"`
	Body         *string      `json:"body"`
	Reason       string       `json:"reason"`
	Evidence     []string     `json:"evidence"`
}

type SkillSuggestion struct {
	SuggestionID SuggestionID `json:"suggestionId"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Scope        string       `json:"scope"`
	Body         *string      `json:"body"`
	Reason       string       `json:"reason"`
	Evidence     []string     `json:"evidence"`
}

type MemorySuggestionsView struct {
	Revision  CatalogRevision    `json:"revision"`
	Available bool               `json:"available"`
	Memories  []MemorySuggestion `json:"memories"`
	Skills    []SkillSuggestion  `json:"skills"`
}

type RememberMemoryInput struct {
	Session SessionRef `json:"session"`
	Scope   string     `json:"scope"`
	Note    string     `json:"note"`
}

type RememberMemoryResult struct {
	MemoryID    MemoryID `json:"memoryId"`
	DisplayPath string   `json:"displayPath"`
	// InvalidationScope is adapter-internal mutation metadata. It never enters
	// the RuntimeAPI or Remote wire payload.
	InvalidationScope CatalogScope `json:"-"`
}

type ForgetMemoryInput struct {
	Session  SessionRef `json:"session"`
	MemoryID MemoryID   `json:"memoryId"`
}

type ForgetMemoryResult struct {
	Forgotten         bool         `json:"forgotten"`
	InvalidationScope CatalogScope `json:"-"`
}

type SaveMemoryDocumentInput struct {
	Session    SessionRef `json:"session"`
	DocumentID DocumentID `json:"documentId"`
	Body       string     `json:"body"`
}

type SaveMemoryDocumentResult struct {
	DocumentID        DocumentID   `json:"documentId"`
	Saved             bool         `json:"saved"`
	InvalidationScope CatalogScope `json:"-"`
}

type AcceptMemorySuggestionInput struct {
	Session          SessionRef      `json:"session"`
	SuggestionID     SuggestionID    `json:"suggestionId"`
	ExpectedRevision CatalogRevision `json:"expectedRevision"`
}

type AcceptMemorySuggestionResult struct {
	MemoryID          MemoryID     `json:"memoryId"`
	InvalidationScope CatalogScope `json:"-"`
}

type AcceptSkillSuggestionInput struct {
	Session          SessionRef      `json:"session"`
	SuggestionID     SuggestionID    `json:"suggestionId"`
	ExpectedRevision CatalogRevision `json:"expectedRevision"`
}

type AcceptSkillSuggestionResult struct {
	SkillID           SkillID      `json:"skillId"`
	InvalidationScope CatalogScope `json:"-"`
}

type ResearchInput struct {
	Session SessionRef `json:"session"`
}

type ResearchCriterion struct {
	CriterionID   CriterionID `json:"criterionId"`
	Description   string      `json:"description"`
	Required      bool        `json:"required"`
	EvidenceCount int         `json:"evidenceCount"`
	Status        string      `json:"status"`
}

type ResearchTask struct {
	TaskID             ResearchTaskID      `json:"taskId"`
	Goal               *string             `json:"goal"`
	Status             string              `json:"status"`
	Iteration          int                 `json:"iteration"`
	CurrentDirection   *string             `json:"currentDirection"`
	StaleCount         int                 `json:"staleCount"`
	PivotCount         int                 `json:"pivotCount"`
	PivotRequired      bool                `json:"pivotRequired"`
	LastHeartbeatAt    string              `json:"lastHeartbeatAt,omitempty"`
	FindingCount       int                 `json:"findingCount"`
	OpenCriteria       []ResearchCriterion `json:"openCriteria"`
	Blocker            *string             `json:"blocker,omitempty"`
	DisplayPath        string              `json:"displayPath,omitempty"`
	NextRequiredAction *string             `json:"nextRequiredAction,omitempty"`
}

type ResearchStatusView struct {
	Available bool          `json:"available"`
	Task      *ResearchTask `json:"task,omitempty"`
}

type ListResearchInput struct {
	Session SessionRef `json:"session"`
	Cursor  Cursor     `json:"cursor,omitempty"`
	Limit   int        `json:"limit,omitempty"`
}

type ResearchPage struct {
	Items   []ResearchTask `json:"items"`
	HasMore bool           `json:"hasMore"`
	Next    Cursor         `json:"next,omitempty"`
}

type ResearchFindingsInput struct {
	Session SessionRef     `json:"session"`
	TaskID  ResearchTaskID `json:"taskId"`
	Cursor  Cursor         `json:"cursor,omitempty"`
	Limit   int            `json:"limit,omitempty"`
}

type ResearchFinding struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Summary   *string  `json:"summary"`
	Source    string   `json:"source"`
	Command   string   `json:"command,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Accepted  bool     `json:"accepted"`
	CreatedAt string   `json:"createdAt"`
}

type ResearchFindingsPage struct {
	Items   []ResearchFinding `json:"items"`
	HasMore bool              `json:"hasMore"`
	Next    Cursor            `json:"next,omitempty"`
}

type ResearchEvidence struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Summary  string   `json:"summary"`
	Source   string   `json:"source"`
	Command  string   `json:"command,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Accepted bool     `json:"accepted"`
}

type RecordResearchEvidenceInput struct {
	Session     SessionRef       `json:"session"`
	TaskID      ResearchTaskID   `json:"taskId"`
	CriterionID CriterionID      `json:"criterionId"`
	Evidence    ResearchEvidence `json:"evidence"`
}

type RecordResearchEvidenceResult struct {
	Recorded bool `json:"recorded"`
}
