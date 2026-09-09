package provider

import "encoding/json"

// ProtocolRecoveryRecord is stored only on a local sentinel. The Message field
// holds raw JSON so unknown versions/fields survive older readers unchanged.
type ProtocolRecoveryRecord struct {
	Evidence    string `json:"evidence,omitempty"`
	Projected   bool   `json:"projected,omitempty"`
	Version     int    `json:"version"`
	ID          string `json:"id"`
	State       string `json:"state"` // pending|consumed
	Scope       string `json:"scope"`
	Fingerprint string `json:"fingerprint"`
	Prefix      int    `json:"prefix"`
	Count       int    `json:"count"`
	Anchor      string `json:"anchor"`
	Run         uint64 `json:"run"`
}

type ProtocolRecoveryAction struct {
	ID string `json:"id"`
}

func DecodeProtocolRecovery(raw json.RawMessage) (ProtocolRecoveryRecord, bool) {
	var r ProtocolRecoveryRecord
	err := json.Unmarshal(raw, &r)
	return r, err == nil && r.Version == 1 && r.ID != "" && r.Prefix > 0 && r.Fingerprint != "" && (r.State == "pending" || r.State == "consumed")
}
