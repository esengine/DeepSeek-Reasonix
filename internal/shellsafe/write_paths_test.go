package shellsafe

import "testing"

func TestStaticWritePathsRejectsIncompleteScopes(t *testing.T) {
	for _, command := range []string{`echo x > a`, `printf '%s' x >> 'a b'`} {
		if paths, ok := StaticWritePaths(command); !ok || len(paths) != 1 {
			t.Fatalf("literal scope: %s %v %v", command, paths, ok)
		}
	}
	for _, command := range []string{`echo $(rm a) > b`, `echo x > "$TARGET"`, `echo x > a; rm b`, `printf -v var x`, `echo x > a &`, `python write.py`, `echo x > a 2>&1`, `echo x > *.txt`} {
		if paths, ok := StaticWritePaths(command); ok {
			t.Fatalf("unproven scope: %s %v", command, paths)
		}
	}
}

func TestGitNoPagerRetainsEffectClassification(t *testing.T) {
	for _, command := range []string{`git --no-pager diff --stat`, `git -C repo --no-pager status`, `git --no-pager -C repo log --oneline`} {
		got := ClassifyBash(command)
		if got.Certainty != EffectKnown || got.Writes != 0 {
			t.Fatalf("reader misclassified: %s %+v", command, got)
		}
	}
	for _, command := range []string{`git --no-pager diff --output=out`, `git --no-pager -c core.pager=evil log`, `git --no-pager diff --ext-diff`, `git --no-pager status; rm file`} {
		got := ClassifyBash(command)
		if got.Certainty == EffectKnown && got.Writes == 0 {
			t.Fatalf("writer classified read-only: %s %+v", command, got)
		}
	}
}
