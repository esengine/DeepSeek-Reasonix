package doctor

import (
	"time"

	"reasonix/internal/agent"
)

// SessionBundleHead is one head of a schema-2 session log as the bundle
// manifest lists it. Names are user text and stay; previews are left out
// because the transcript files themselves travel in the bundle.
type SessionBundleHead struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Name         string    `json:"name,omitempty"`
	ParentHead   string    `json:"parent_head,omitempty"`
	ForkFrom     string    `json:"fork_from,omitempty"`
	Writer       string    `json:"writer,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`
	MessageCount int       `json:"message_count"`
	Turns        int       `json:"turns,omitempty"`
	Selected     bool      `json:"selected,omitempty"`
	Covered      bool      `json:"covered,omitempty"`
	Retired      bool      `json:"retired,omitempty"`
}

// describeSessionBundleHeads fills the head fields of a manifest entry from
// the session's own log. A schema-1 session leaves them empty; an unreadable
// schema-2 log is reported to the caller and still bundled as files.
func describeSessionBundleHeads(entry *SessionBundleEntry, path string) error {
	heads, err := agent.ListSessionHeads(path)
	if err != nil {
		return err
	}
	if len(heads) == 0 {
		return nil
	}
	entry.LogFormat = 2
	entry.Heads = make([]SessionBundleHead, 0, len(heads))
	for _, head := range heads {
		if head.Selected {
			entry.SelectedHead = head.ID
		}
		entry.Heads = append(entry.Heads, SessionBundleHead{
			ID: head.ID, Kind: head.Kind, Name: head.Name, ParentHead: head.ParentHead, ForkFrom: head.ForkFrom,
			Writer: head.Writer, CreatedAt: head.CreatedAt, LastActivity: head.LastActivity,
			MessageCount: head.MessageCount, Turns: head.Turns,
			Selected: head.Selected, Covered: head.Covered, Retired: head.Retired,
		})
	}
	return nil
}
