package anthropic

import "strings"

// metadataConfig is Anthropic's metadata envelope; only user_id is set.
type metadataConfig struct {
	UserID string `json:"user_id"`
}

// userMetadata wraps a non-empty attribution id into the Anthropic metadata
// shape; empty returns nil so no spurious metadata block is sent.
func userMetadata(userID string) *metadataConfig {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	return &metadataConfig{UserID: userID}
}
