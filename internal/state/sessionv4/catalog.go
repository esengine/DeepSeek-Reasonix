package sessionv4

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Listing is what a session list shows for a conversation.
type Listing struct {
	Title    string
	Preview  string
	Turns    int
	ModelRef string
}

type catalogMetadata struct {
	Version      int    `json:"version"`
	Codec        string `json:"codec"`
	SessionID    string `json:"sessionId"`
	CreatedAt    string `json:"createdAt"`
	Title        string `json:"title"`
	ModelRef     string `json:"modelRef"`
	Turns        int    `json:"turns"`
	Preview      string `json:"preview"`
	LogSize      int64  `json:"logSize"`
	LogModTimeNS int64  `json:"logModTimeNs"`
}

// catalogVersion is the listing cache 1.x writes today; a cache of any other
// version, or one built from bytes the log no longer holds, is stale.
const catalogVersion = 5

// CachedListing reads the listing 1.x cached for the session, when that cache
// still describes the log as it is.
func (s Session) CachedListing() (Listing, bool) {
	raw, err := fs.ReadFile(s.store, path.Join(".query-cache", s.Manifest.SessionID, "catalog-metadata.json"))
	if err != nil {
		return Listing{}, false
	}
	var m catalogMetadata
	if json.Unmarshal(raw, &m) != nil ||
		m.Version != catalogVersion || m.Codec != codec || m.SessionID != s.Manifest.SessionID ||
		m.CreatedAt != s.Manifest.CreatedAt.UTC().Format(time.RFC3339Nano) ||
		m.LogSize != s.LogSize || m.LogModTimeNS != s.LogModTime.UnixNano() {
		return Listing{}, false
	}
	return Listing{Title: strings.TrimSpace(m.Title), Preview: m.Preview, Turns: m.Turns, ModelRef: m.ModelRef}, true
}
