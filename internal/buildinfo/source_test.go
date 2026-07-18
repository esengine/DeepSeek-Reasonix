package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestRevisionFromSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{name: "missing"},
		{name: "clean", settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.modified", Value: "false"}}, want: "abc123"},
		{name: "dirty", settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.revision", Value: "abc123"}}, want: "abc123+dirty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := revisionFromSettings(tt.settings); got != tt.want {
				t.Fatalf("revisionFromSettings() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInjectedSourceRevisionWins(t *testing.T) {
	old := SourceRevision
	SourceRevision = " injected-revision "
	t.Cleanup(func() { SourceRevision = old })
	if got := Revision(); got != "injected-revision" {
		t.Fatalf("Revision() = %q", got)
	}
}
