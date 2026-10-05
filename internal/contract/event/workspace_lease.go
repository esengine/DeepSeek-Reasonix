package event

// WorkspaceLease projects a claim wait or a closed contention account.
// Paths names the blocking extent; RequestedPaths names the granted or
// requested extent, including the workspace root for a whole-workspace claim.
type WorkspaceLease struct {
	Contended       int      `json:"contended"`
	Reported        int      `json:"reported,omitempty"`
	WaitedMs        int64    `json:"waitedMs,omitempty"`
	HeldMs          int64    `json:"heldMs"`
	IdleMs          int64    `json:"idleMs"`
	Holder          string   `json:"holder,omitempty"`
	HolderSessionID string   `json:"holderSessionId,omitempty"`
	Paths           []string `json:"paths,omitempty"`
	RequestedPaths  []string `json:"requestedPaths,omitempty"`
}
