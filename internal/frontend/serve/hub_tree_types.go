package serve

// treeWorkspace is one folder in the sidebar: what it is called, whether a pane
// is driving it, and the conversations saved under it.
type treeWorkspace struct {
	ListingState    string `json:"listingState,omitempty"`
	ListingRevision uint64 `json:"listingRevision,omitempty"`
	CountsComplete  *bool  `json:"countsComplete,omitempty"`
	PendingMetadata int    `json:"pendingMetadata,omitempty"`
	FailedMetadata  int    `json:"failedMetadata,omitempty"`
	RecentOffset    *int   `json:"recentOffset,omitempty"`
	Root            string `json:"root"`
	Name            string `json:"name"`
	// WorkspaceRoots are the working directories belonging to this project.
	WorkspaceRoots []string `json:"workspaceRoots,omitempty"`
	Isolated       bool     `json:"isolated,omitempty"`
	Missing        bool     `json:"missing,omitempty"`
	Open           bool     `json:"open,omitempty"`
	// Remembered marks a folder the user chose. A window always has a root — the
	// one it happened to launch in — and a client that cannot tell the two apart
	// shows "never picked a project" as if it were a project.
	Remembered bool `json:"remembered,omitempty"`
	// Total counts every conversation the workspace lists, loaded or not, and
	// Archived the share of them that are archived. Sessions is the newest of
	// them, as many as the request's window asked for.
	Total    int `json:"total"`
	Archived int `json:"archived,omitempty"`
	// Remaining counts the rows the request's scope and query match that
	// Sessions does not carry: what "show more" would still bring. Always sent, so
	// a client can tell a kernel that predates it from one with nothing left.
	Remaining int           `json:"remaining"`
	Sessions  []treeSession `json:"sessions"`
}

// treeSession is one conversation. RuntimeID is set when a pane already has it
// open, which is what lets the sidebar focus that pane instead of opening a
// second writer for the same transcript.
type treeSession struct {
	MetadataState string `json:"metadataState,omitempty"`
	CountsKnown   *bool  `json:"countsKnown,omitempty"`
	Path          string `json:"path"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	Isolated      bool   `json:"isolated,omitempty"`
	Name          string `json:"name"`
	Title         string `json:"title,omitempty"`
	Turns         int    `json:"turns,omitempty"`
	RuntimeID     string `json:"runtimeId,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
	Pinned        bool   `json:"pinned,omitempty"`
	// LastActiveAt is unix milliseconds of the last write to the conversation,
	// which is what a recency-ordered list sorts by; absent when unknown.
	LastActiveAt int64 `json:"lastActiveAt,omitempty"`
	// AutoArchivedAt is unix milliseconds of an automatic archive, else absent.
	AutoArchivedAt int64 `json:"autoArchivedAt,omitempty"`
	// Unread is a turn that finished since the person last looked. The kernel
	// derives it from two stored timestamps; absence reads as seen.
	Unread bool `json:"unread,omitempty"`
	// ViewedAt is unix milliseconds of the last time the person looked, sent
	// only while the row is unread: it is where the new answers begin.
	ViewedAt int64 `json:"viewedAt,omitempty"`
	// Legacy marks a conversation kept in the 1.x store: this build reads that
	// log and never writes it, so the row says so rather than looking native.
	Legacy bool `json:"legacy,omitempty"`
	// Copies are this conversation's conflict-recovery copies. A save that
	// keeps conflicting writes one file per turn, all under the one title, and
	// unfolded that is a sidebar of rows the user never made.
	Copies []treeSession `json:"copies,omitempty"`
	// Versions are this conversation as it stood before each rewind cut it,
	// newest first. Each is a whole conversation and opens like one.
	Versions []treeSession `json:"versions,omitempty"`
}
