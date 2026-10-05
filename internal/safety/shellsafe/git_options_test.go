package shellsafe

import "testing"

func TestGitLocationOptionsClassifyAsTheSubcommand(t *testing.T) {
	readOnly := []string{
		"git -C . status --short --branch", "git -C sub log -3", "git --no-pager log -3",
		"git -P diff", "git -C a -C b --no-pager show HEAD", "git --no-pager tag -l",
	}
	for _, c := range readOnly {
		base, sub, fields, ok := ClassifyReadOnlyCommand(c)
		if !ok || ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
			t.Errorf("%q: classified=%v writes=%v, want a read", c, ok, ok && ArgsMakeReadOnlyCommandWrite(base, sub, fields))
		}
	}
	writes := []string{
		"git -C . commit -m x", "git -C . push", "git --no-pager log --output=f",
		"git -C . tag v1", "git -C", "git -C .", "git --no-pager",
		"git -c core.fsmonitor=evil status", "git -c core.pager=cat log",
		"git --git-dir=/x status", "git --work-tree=/x status", "git --exec-path=/x status",
		"git --config-env=core.pager=X log", "git -C . -c core.fsmonitor=x status",
	}
	for _, c := range writes {
		base, sub, fields, ok := ClassifyReadOnlyCommand(c)
		if ok && !ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
			t.Errorf("%q classified as a read (base %q sub %q)", c, base, sub)
		}
	}
}
