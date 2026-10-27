package gitcmd

import (
	"slices"
	"testing"
)

func TestRepositoryProgramsNamesWhatTheRepositoryRunsOnReads(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	if got, err := RepositoryPrograms(f.ctx, f.dir); err != nil || len(got) != 0 {
		t.Fatalf("a repository with no program keys: got %v, %v", got, err)
	}
	f.appendConfig("config", "[core]\n\tfsmonitor = /tmp/x\n[diff \"d\"]\n\ttextconv = /tmp/x\n[filter \"f\"]\n\tclean = /tmp/x\n[diff]\n\texternal = /tmp/x\n[gpg]\n\tprogram = /tmp/x\n[user]\n\tname = fine\n")
	got, err := RepositoryPrograms(f.ctx, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"core.fsmonitor", "diff.d.textconv", "filter.f.clean", "diff.external", "gpg.program"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s was not reported; got %v", want, got)
		}
	}
	if slices.Contains(got, "user.name") {
		t.Errorf("an ordinary key was reported: %v", got)
	}
}
