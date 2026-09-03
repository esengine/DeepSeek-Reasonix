package event

// CacheDiagnostics describes whether and why the cacheable prefix changed.
type CacheDiagnostics struct {
	PrefixHash          string
	PrefixChanged       bool
	PrefixChangeReasons []string // "system", "tools", "log_rewrite", "session_context"
	SystemHash          string
	ToolsHash           string
	LogRewriteVersion   int
	ToolSchemaTokens    int
	CacheMissTokens     int
	CacheHitTokens      int
	SessionContext      *SessionContextDiagnostics
}

// SessionContextSectionDiagnostics is a content-free fingerprint.
type SessionContextSectionDiagnostics struct {
	Digest string
	Chars  int
}

// SessionContextDiagnostics attributes a snapshot change without logging bodies.
type SessionContextDiagnostics struct {
	Version          int
	Digest           string
	TargetRole       string
	Reasons          []string
	Environment      SessionContextSectionDiagnostics
	Workspace        SessionContextSectionDiagnostics
	BackgroundMemory SessionContextSectionDiagnostics
	SkillsCatalog    SessionContextSectionDiagnostics
}
