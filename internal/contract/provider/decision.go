package provider

// DecisionReceipt is durable, provider-excluded evidence of a user-owned
// approval decision. It intentionally contains only bounded labels and the
// outcome, never free-form guidance or provider-visible content.
type DecisionReceipt struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Subject string `json:"subject,omitempty"`
	Outcome string `json:"outcome"`
	// Via names the paired device the decision was made on; nil is the window.
	Via *Via `json:"via,omitempty"`
}

// Via is the paired device a person acted from. Declared by the host that
// authenticated the device, never read off the input: a message cannot say
// where it was typed.
type Via struct {
	// Device is the pairing's id, stable while the device stays paired.
	Device string `json:"device"`
	// Ordinal is the number the window listed the device under when it acted,
	// the 设备 N both sides showed at the time.
	Ordinal int `json:"ordinal"`
}
