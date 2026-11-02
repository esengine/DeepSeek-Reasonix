package testscope

import "testing"

func TestInstalledHomesRestoreInReverseOrder(t *testing.T) {
	if got := Home(); got != "" {
		t.Fatalf("uninstalled home = %q", got)
	}
	t.Run("outer", func(t *testing.T) {
		t.Cleanup(InstallHome("outer"))
		if got := Home(); got != "outer" {
			t.Fatalf("installed home = %q, want outer", got)
		}
		t.Run("inner", func(t *testing.T) {
			t.Cleanup(InstallHome("inner"))
			if got := Home(); got != "inner" {
				t.Fatalf("nested home = %q, want inner", got)
			}
		})
		if got := Home(); got != "outer" {
			t.Fatalf("restored home = %q, want outer", got)
		}
	})
	if got := Home(); got != "" {
		t.Fatalf("home after restoration = %q", got)
	}
}
