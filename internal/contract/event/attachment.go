package event

// Attachment is an image a message references that the host stored, or that
// the session was handed outside the workspace. A workspace image carries a
// workspace-relative Path and an empty Ref; an external one carries the
// absolute location a paste reads back, and Ref is the opaque external-folder
// token the surface hides from the visible body now that it previews by Path.
type Attachment struct {
	Path string
	Ref  string
}
