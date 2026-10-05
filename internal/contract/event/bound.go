package event

// BoundKind says what happened to a tool result on its way into the model's
// context. The distinction is load-bearing for a frontend: spilled and windowed
// output is still reachable, truncated output is gone from what the model saw.
type BoundKind uint8

const (
	BoundWhole     BoundKind = iota // fit as-is; Output is the complete result
	BoundSpilled                    // moved to a file; Output is the pointer the model got
	BoundWindowed                   // a leading window of a paged read; the rest is one call away
	BoundTruncated                  // middle discarded; only KeptBytes reached the model
)

// OutputBound describes how a tool result was fitted into context. One named
// sub-state rather than independent flags: "truncated and spilled" is not a
// state anything can produce, so no type should be able to express it.
type OutputBound struct {
	Kind      BoundKind
	Lines     int    // the original's line count
	Bytes     int    // the original's size in bytes
	KeptBytes int    // what reached the model (BoundTruncated)
	Path      string // where the full text lives (BoundSpilled)
}

// Lossy reports whether content the model never saw was discarded outright.
func (b OutputBound) Lossy() bool { return b.Kind == BoundTruncated }
