package event

// ReadStatusPayload is the host-only delivery state of one logical read. It is
// keyed by turn, read id, and generation, and holds ranges only: never source
// text, never a whole-file digest presented as a percentage.
// Covered and Missing use zero-based half-open source ranges, like read
// envelopes and pause receipts. Consumers perform one-based display conversion.
type ReadStatusPayload struct {
	ReadID     string   `json:"read_id"`
	Generation uint64   `json:"generation,omitempty"`
	Sequence   uint64   `json:"sequence,omitempty"`
	Path       string   `json:"path"`
	Intent     string   `json:"intent,omitempty"`
	State      string   `json:"state"`
	Covered    [][2]int `json:"covered,omitempty"`
	Missing    [][2]int `json:"missing,omitempty"`
	SourceEnd  *int     `json:"source_end,omitempty"`
	HasMore    bool     `json:"has_more,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Recovery   string   `json:"recovery,omitempty"`
	Active     bool     `json:"active,omitempty"`
}
