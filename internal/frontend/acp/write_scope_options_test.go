package acp

import (
	"path/filepath"
	"strings"
	"testing"
)

func writeScopeIDs(options []PermissionOption) []string {
	var out []string
	for _, o := range options {
		out = append(out, o.OptionID)
	}
	return out
}

func TestWriteScopeOptionsOfferOnlyWhatTheHostWillHonour(t *testing.T) {
	file := filepath.Join("/srv", "app", "wsl.conf")
	cases := []struct {
		session, persist bool
		want             []string
	}{
		{false, false, []string{string(OptAllowOnce), string(OptRejectOnce)}},
		{true, false, []string{string(OptAllowOnce), string(OptAllowAlways), string(OptRejectOnce)}},
		{true, true, []string{string(OptAllowOnce), string(OptAllowAlways), string(optAllowPersist), string(OptRejectOnce)}},
	}
	for _, c := range cases {
		got := writeScopeIDs(writeScopeOptions(file, c.session, c.persist))
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("session=%v persist=%v: got %v, want %v", c.session, c.persist, got, c.want)
		}
	}
}

func TestWriteScopeOptionsNameTheFolderAWiderAnswerCovers(t *testing.T) {
	file := filepath.Join("/srv", "app", "wsl.conf")
	for _, o := range writeScopeOptions(file, true, true) {
		wide := o.OptionID == string(OptAllowAlways) || o.OptionID == string(optAllowPersist)
		if wide && !strings.Contains(o.Name, filepath.Join("/srv", "app")) {
			t.Errorf("option %q does not say which folder it covers: %q", o.OptionID, o.Name)
		}
	}
}
