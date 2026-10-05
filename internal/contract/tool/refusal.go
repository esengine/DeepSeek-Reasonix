// refusal.go — the identity a host refusal travels under, so a caller that has
// to tell two refusals apart is never left matching the sentence one of them
// was phrased in this release.
package tool

import "strings"

// Refusal is one host refusal. Code is the identity and the only thing a reader
// may classify on; Message is presentation and may be reworded or translated
// without changing what the refusal means. Whoever first knows why the call
// cannot proceed owns the code — a table of excuses kept elsewhere is half the
// contract, drifting from the state that decides it.
type Refusal struct {
	Code    string
	Message string
}

// Error lets one value serve both refusal paths a contextual tool has: the
// availability gate that gets asked, and the Execute that fails closed on a
// stale transcript. Two spellings of one fact is how the identity comes to be
// carried on one path and lost on the other.
func (r Refusal) Error() string { return r.String() }

// String renders the refusal for the model. The code travels with the sentence
// because an identity the model never sees is one it cannot cite back.
func (r Refusal) String() string {
	msg := strings.TrimSpace(r.Message)
	if r.Code == "" {
		return msg
	}
	if msg == "" {
		return "(refusal: " + r.Code + ")"
	}
	return msg + " (refusal: " + r.Code + ")"
}

// Empty reports a refusal carrying neither identity nor words, which is what a
// tool that cannot say why it is unavailable returns.
func (r Refusal) Empty() bool { return r.Code == "" && strings.TrimSpace(r.Message) == "" }
