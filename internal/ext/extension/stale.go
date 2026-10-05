package extension

// StaleGeneration reports whether a message generation is older than the
// currently published runtime generation and must be silently dropped.
// Equal or newer generations are not stale: mid-activation UI/stream traffic
// for the generation about to be published must still be accepted.
func StaleGeneration(messageGen, publishedGen uint64) bool {
	return messageGen != 0 && publishedGen != 0 && messageGen < publishedGen
}
