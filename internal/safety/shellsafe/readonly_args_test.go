package shellsafe

import "testing"

func TestArgsMakeReadOnlyCommandWrite(t *testing.T) {
	cases := []struct {
		name  string
		base  string
		sub   string
		argv  []string
		write bool
	}{
		{"plain find reads", "find", "", []string{"find", ".", "-name", "*.go"}, false},
		{"find -delete writes", "find", "", []string{"find", ".", "-delete"}, true},
		{"find -exec writes", "find", "", []string{"find", ".", "-exec", "rm", "{}", ";"}, true},
		{"plain sort reads", "sort", "", []string{"sort", "-n", "f"}, false},
		{"sort -o writes", "sort", "", []string{"sort", "-o", "out", "f"}, true},
		{"sort -ofile writes", "sort", "", []string{"sort", "-oout", "f"}, true},
		{"git diff reads", "git", "diff", []string{"git", "diff", "f"}, false},
		{"git diff --output writes", "git", "diff", []string{"git", "diff", "--output=x", "f"}, true},
		{"go env reads", "go", "env", []string{"go", "env", "GOPATH"}, false},
		{"go env -w writes", "go", "env", []string{"go", "env", "-w", "GOFLAGS=-mod=mod"}, true},
		{"bare git tag lists", "git", "tag", []string{"git", "tag"}, false},
		{"git tag -l lists", "git", "tag", []string{"git", "tag", "-l", "v1.*"}, false},
		{"git tag with a name creates", "git", "tag", []string{"git", "tag", "v1.0.0"}, true},
		{"git tag -d deletes", "git", "tag", []string{"git", "tag", "-d", "v1.0.0"}, true},
		{"git tag --sort still lists", "git", "tag", []string{"git", "tag", "--sort=-v:refname"}, false},
		{"a reader with no rule", "cat", "", []string{"cat", "f"}, false},
		{"empty argv", "cat", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ArgsMakeReadOnlyCommandWrite(tc.base, tc.sub, tc.argv); got != tc.write {
				t.Errorf("ArgsMakeReadOnlyCommandWrite(%q, %q, %v) = %v, want %v", tc.base, tc.sub, tc.argv, got, tc.write)
			}
		})
	}
}

// The rule used to live in three packages, and they had already drifted: only
// permission knew that `git tag v1.0.0` writes a ref. Whatever the tables say,
// every classifier must now say it together.
func TestGitTagCreationIsAWriteEverywhere(t *testing.T) {
	base, sub, fields, ok := ClassifyReadOnlyCommand("git tag v1.0.0")
	if !ok {
		t.Fatal("git tag must classify against the read-only tables so its arguments get judged")
	}
	if !ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
		t.Error("creating a tag writes the ref namespace")
	}
	base, sub, fields, ok = ClassifyReadOnlyCommand("git tag -l")
	if !ok || ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
		t.Error("listing tags reads")
	}
}
