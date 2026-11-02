package evidence

import "testing"

// A command the shell grammar cannot read is unknown, and only observing the
// workspace settles it; a read-only git query is none whatever its exit status.
func TestWhichCommandsLeaveAnExtentOnlyObservationCanSettle(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
	}{
		{"$repo='D:\\x'; foreach($d in 'a','b'){ git -C $repo status; Write-Host $d }", MutationUnknown},
		{"Test-Path docs/a.md", MutationUnknown},
		{"git rev-parse --show-toplevel", MutationNone},
		{"git status", MutationNone},
		{"Get-Content a.md", MutationNone},
	} {
		if got := bashMutationClass(tc.command); got != tc.want {
			t.Errorf("%q is %s, want %s", tc.command, got, tc.want)
		}
	}
}
