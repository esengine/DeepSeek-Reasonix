package main

import "time"

// P2-2e-natural, in the source so that moving it is a diff someone has to
// justify rather than a flag chosen once the numbers are visible. A separate,
// completed assay (P2-2d-designed, below) answered a different question from a
// written task list; mixing its turns in would put two sampling mechanisms in
// one denominator.
const (
	// frozenAt is when this protocol was fixed — after the designed assay ran,
	// so its turns cannot enter this sample even if they shared a session root.
	frozenAt = "2026-09-08T04:53:31Z"
	// sampleSize is the whole sample. Not a minimum: collecting past it because
	// the answer sits near a threshold is the optional stopping this guards.
	sampleSize = 20
	// designedSample is false here: these turns are whatever ordinary Studio use
	// produces. Turns authored for this experiment do not belong in it.
	designedSample = false
	// The NO-GO gate, unchanged since before any sample existed. Both halves
	// required: a partition can be coarse and still rare, or common and still
	// thin, and those are different products.
	singletonShareGate = 0.50
	richTurnShareGate  = 0.40
	// richTurn is a group worth offering as one fold.
	richTurnCalls = 3
)

// designedReference is the completed P2-2d-designed assay, kept so the natural
// sample can be read against it — a reference condition, never a baseline to
// test against: neither randomized nor paired, and both small, so the contrast
// is a plain difference. Raw data: ~/.reasonix/research/p2-2d-designed/.
var designedReference = struct {
	Turns          int
	Groups         int
	SingletonShare float64
	RichTurnShare  float64
	ZeroCallTurns  int
	Note           string
}{
	Turns: 12, Groups: 18, SingletonShare: 3.0 / 18.0, RichTurnShare: 8.0 / 12.0, ZeroCallTurns: 0,
	Note: "designed stress: half the turns routed plan-first, every turn called a tool",
}

func frozenTime() time.Time {
	t, err := time.Parse(time.RFC3339, frozenAt)
	if err != nil {
		panic("workgroupassay: frozenAt is not a timestamp: " + err.Error())
	}
	return t
}
