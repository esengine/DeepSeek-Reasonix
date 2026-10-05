package boot

import (
	"testing"

	"reasonix/internal/ext/extension"
)

// The one-time legacy upgrades are skipped for a build that continues a live
// generation. Reading that from Owner would be wrong in the dangerous
// direction: build() adopts an owner for every build, so a first launch would
// classify itself as a continuation and skip the very import it exists to run.
func TestContinuesGenerationIgnoresAdoptedOwner(t *testing.T) {
	cold := Options{}
	cold.Owner = extension.NewRuntimeOwner()
	if continuesGeneration(cold) {
		t.Error("a cold build that adopted an owner must still run the first-launch upgrades")
	}
	if continuesGeneration(Options{}) {
		t.Error("a bare cold build must run the first-launch upgrades")
	}

	rebuild := Options{}
	rebuild.PreviousSnapshot = &extension.RuntimeSnapshot{}
	if !continuesGeneration(rebuild) {
		t.Error("a rebuild repeats upgrades its own predecessor already ran")
	}
}
