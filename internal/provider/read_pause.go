package provider

// ReadPause is a display-only terminal receipt. It carries no source text,
// cursor or executable evidence and cannot resume a historical read.
type ReadPause struct {
	ID      string       `json:"id"`
	Reads   []PausedRead `json:"reads"`
	Omitted int          `json:"omitted,omitempty"`
}

type PausedRead struct {
	ReadID  string   `json:"readId"`
	Path    string   `json:"path"`
	Intent  string   `json:"intent,omitempty"`
	Covered [][2]int `json:"covered,omitempty"`
	Missing [][2]int `json:"missing,omitempty"`
	Reason  string   `json:"reason"`
}
