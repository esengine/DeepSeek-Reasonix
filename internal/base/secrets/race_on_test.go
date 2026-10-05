//go:build race

package secrets

// raceDetector is whether this test binary was built with -race, which slows
// regexp work by an order of magnitude.
const raceDetector = true
