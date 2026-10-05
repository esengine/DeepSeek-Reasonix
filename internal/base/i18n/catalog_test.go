package i18n

import "testing"

// The desktop shell reads a catalogue instead of installing one, because its
// interface language is a separate setting from the kernel's and both live in
// one process. Reading must therefore never disturb the active catalogue.
func TestCatalogDoesNotInstall(t *testing.T) {
	DetectLanguage("en")
	before := CurrentLanguage()
	if got := Catalog("zh").PickWorkspaceTitle; got == English.PickWorkspaceTitle {
		t.Errorf("Catalog(zh) returned the English text: %q", got)
	}
	if CurrentLanguage() != before {
		t.Errorf("reading a catalogue changed the active language: %q → %q", before, CurrentLanguage())
	}
	if M.PickWorkspaceTitle != English.PickWorkspaceTitle {
		t.Error("reading a catalogue swapped M")
	}
	// An unset or unknown tag falls back to whatever is active, not to a guess.
	if Catalog("").PickWorkspaceTitle != M.PickWorkspaceTitle {
		t.Error("empty tag should follow the active catalogue")
	}
}
