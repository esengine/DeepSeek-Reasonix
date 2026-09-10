package provider

// ReadCompletion retains terminal coverage for diagnostics, never executable
// evidence. Missing fields in older transcripts are safe zero values.
type ReadCompletion struct {
	ID      string          `json:"id"`
	Reads   []CompletedRead `json:"reads"`
	Omitted int             `json:"omitted,omitempty"`
}

type CompletedRead struct {
	ReadID    string   `json:"read_id"`
	Path      string   `json:"path"`
	Snapshot  string   `json:"snapshot,omitempty"`
	Intent    string   `json:"intent"`
	Verdict   string   `json:"verdict"`
	Covered   [][2]int `json:"covered"`
	SourceEnd *int     `json:"source_end,omitempty"`
}
