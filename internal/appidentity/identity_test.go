package appidentity

import (
	"strings"
	"testing"
)

func TestAppUserModelIDIsStableAndVersionIndependent(t *testing.T) {
	if AppUserModelID != "io.reasonix.desktop" {
		t.Fatalf("AppUserModelID = %q, want stable desktop identity %q", AppUserModelID, "io.reasonix.desktop")
	}
	if strings.ContainsAny(AppUserModelID, " \t\r\n") || len(AppUserModelID) > 128 {
		t.Fatalf("invalid AppUserModelID %q", AppUserModelID)
	}
}

func TestAppUserModelIDDoesNotMergeStudio(t *testing.T) {
	for _, studioID := range []string{legacyAppUserModelID, studioAppUserModelID} {
		if AppUserModelID == studioID {
			t.Fatalf("desktop identity must not share Studio identity %q", studioID)
		}
	}
}

func TestAppUserModelIDDoesNotMergeLegacyTauriDesktop(t *testing.T) {
	if AppUserModelID == legacyTauriAppUserModelID {
		t.Fatalf("current AppUserModelID %q must remain distinct from Reasonix Desktop 0.53", AppUserModelID)
	}
}
