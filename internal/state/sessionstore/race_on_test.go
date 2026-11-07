//go:build race

package sessionstore

// raceDetector is whether this test binary was built with -race, which turns
// off the sync.Pool reuse that regexp matching relies on, so allocation counts
// stop measuring the code under test.
const raceDetector = true
