package eventwire

import "reasonix/internal/event"

// ReadStatus is the JSON form of event.ReadStatusPayload.
type ReadStatus struct {
	ReadID     string   `json:"readId"`
	Generation uint64   `json:"generation,omitempty"`
	Sequence   uint64   `json:"seq,omitempty"`
	Path       string   `json:"path"`
	Intent     string   `json:"intent,omitempty"`
	State      string   `json:"state"`
	Covered    [][2]int `json:"covered,omitempty"`
	Missing    [][2]int `json:"missing,omitempty"`
	SourceEnd  *int     `json:"sourceEnd,omitempty"`
	HasMore    bool     `json:"hasMore,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Recovery   string   `json:"recovery,omitempty"`
	Active     bool     `json:"active,omitempty"`
}

func toWireReadStatus(in *event.ReadStatusPayload) *ReadStatus {
	if in == nil {
		return nil
	}
	return &ReadStatus{
		ReadID: in.ReadID, Generation: in.Generation, Sequence: in.Sequence,
		Path: in.Path, Intent: in.Intent, State: in.State,
		Covered: in.Covered, Missing: in.Missing, SourceEnd: in.SourceEnd,
		HasMore: in.HasMore, Reason: in.Reason, Recovery: in.Recovery, Active: in.Active,
	}
}
