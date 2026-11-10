package evidence

import (
	"strings"
	"unicode/utf8"
)

// unprovenSegmentLimit bounds the segment a block quotes back. A whole command
// echoed into the transcript is noise; the offending head of it is the fix.
const unprovenSegmentLimit = 120

// bashUnprovenSegment returns the first segment the host cannot prove leaves the
// workspace alone. Not proving it is not the same as knowing it writes: an
// unrecognized command is simply outside what the host can classify, and the
// caller must stay conservative either way.
func bashUnprovenSegment(command string) (string, bool) {
	class, segment := bashScanMutation(command)
	return segment, class != MutationNone
}

func bashMayMutate(command string) bool {
	return bashMutationClass(command) != MutationNone
}

func boundedSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if len(segment) > unprovenSegmentLimit {
		cut := unprovenSegmentLimit
		for cut > 0 && !utf8.RuneStart(segment[cut]) {
			cut--
		}
		segment = segment[:cut] + "…"
	}
	return segment
}
