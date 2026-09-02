package provider

// MessageOrigin records who authored a persisted user-role message. The field
// is host-local provenance: provider projections remove it before serialization.
// Empty means a legacy session whose origin must be inferred conservatively.
type MessageOrigin string

const (
	MessageOriginUser MessageOrigin = "user"
	MessageOriginHost MessageOrigin = "host"
)
