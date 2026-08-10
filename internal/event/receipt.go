package event

// CompletionReceipt is the user-facing completion record on TurnDone: what the
// host could verify about the turn's work, and — the part no prose reliably
// carries — what it could not. Unlike the shadow audit this holds content,
// because a receipt naming no file and no command tells the reader nothing.
type CompletionReceipt struct {
	Verdict       string
	Changes       []ReceiptChange
	Verifications []ReceiptVerification
	Gaps          []ReceiptGap
	Risks         []string
}

// ReceiptChange is one mutated path and whether anything looked at it again.
type ReceiptChange struct {
	Path     string
	Reviewed bool
}

// ReceiptVerification is a verification command's last outcome. Stale means it
// ran before the newest change, so it proves nothing about the current tree.
type ReceiptVerification struct {
	Command string
	Passed  bool
	Stale   bool
}

// ReceiptGap is one thing the receipt refuses to present as verified.
type ReceiptGap struct {
	Kind   string
	Detail string
}
