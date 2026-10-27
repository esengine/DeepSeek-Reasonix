package shellsafe

import (
	"slices"
	"testing"
)

func TestGitContentReadsNameTheRepositoriesTheyReadFrom(t *testing.T) {
	for _, c := range []struct {
		cmd     string
		dirs    []string
		unknown bool
	}{
		{"git diff", []string{""}, false},
		{"git log -p -3", []string{""}, false},
		{"git -C sub status", []string{"sub"}, false},
		{"git --no-pager blame f.go", []string{""}, false},
		{"git status && git diff HEAD", []string{"", ""}, false},
		{"git rev-parse HEAD", nil, false},
		{"git for-each-ref", nil, false},
		{"git branch", nil, false},
		{"ls", nil, false},
		{"cd /elsewhere && git diff", nil, true},
		{"git --git-dir=/x diff", nil, true},
		{"git -c core.fsmonitor=/x diff", nil, true},
	} {
		dirs, unknown := GitContentReadTargets(c.cmd)
		if unknown != c.unknown || !slices.Equal(dirs, c.dirs) {
			t.Errorf("%q: dirs %v unknown %v, want %v %v", c.cmd, dirs, unknown, c.dirs, c.unknown)
		}
	}
}
