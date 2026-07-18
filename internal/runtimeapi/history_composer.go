package runtimeapi

type UnsubscribeSessionInput struct {
	Session SessionRef `json:"session"`
}

type HistoryInput struct {
	Session   SessionRef `json:"session"`
	Cursor    Cursor     `json:"cursor"`
	PageTurns int        `json:"pageTurns"`
}

// ContentRef is an opaque, short-lived capability. RuntimeAPI callers may use
// it only with SessionContent; it conveys no path or transport identity.
type ContentInput struct {
	ContentRef ContentRef `json:"contentRef"`
	Offset     int64      `json:"offset"`
}

type ContentChunk struct {
	ContentRef ContentRef `json:"contentRef"`
	Offset     int64      `json:"offset"`
	Data       []byte     `json:"data"`
	NextOffset *int64     `json:"nextOffset,omitempty"`
	TotalBytes int64      `json:"totalBytes"`
	SHA256     string     `json:"sha256"`
	Encoding   string     `json:"encoding"`
}

type SlashArgsInput struct {
	Session SessionRef `json:"session"`
	Input   string     `json:"input"`
}

type SlashArgItem struct {
	Label   string `json:"label"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint,omitempty"`
	Descend bool   `json:"descend"`
}

type SlashArgsResult struct {
	Items []SlashArgItem `json:"items"`
	From  int            `json:"from"`
}

type PromptHistoryInput struct {
	WorkspaceID WorkspaceID `json:"workspaceId"`
	Cursor      Cursor      `json:"cursor,omitempty"`
	Limit       int         `json:"limit,omitempty"`
}

type PromptHistoryEntry struct {
	Text     string     `json:"text"`
	AtMillis int64      `json:"atMillis"`
	Session  SessionRef `json:"session"`
	Turn     int        `json:"turn"`
}

type PromptHistoryPage struct {
	Entries []PromptHistoryEntry `json:"entries"`
	HasMore bool                 `json:"hasMore"`
	Next    Cursor               `json:"next,omitempty"`
}
