package eventwire

type SessionContextSectionDiagnostics struct {
	Digest string `json:"digest,omitempty"`
	Chars  int    `json:"chars,omitempty"`
}

type SessionContextDiagnostics struct {
	Version          int                              `json:"version"`
	Digest           string                           `json:"digest"`
	TargetRole       string                           `json:"targetRole"`
	Reasons          []string                         `json:"reasons,omitempty"`
	Environment      SessionContextSectionDiagnostics `json:"environment"`
	Workspace        SessionContextSectionDiagnostics `json:"workspace"`
	BackgroundMemory SessionContextSectionDiagnostics `json:"backgroundMemory"`
	SkillsCatalog    SessionContextSectionDiagnostics `json:"skillsCatalog"`
}
