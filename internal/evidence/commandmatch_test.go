package evidence

import "testing"

func TestCommandMatches(t *testing.T) {
	cases := []struct {
		name  string
		cited string
		ran   string
		want  bool
	}{
		{"exact", "go test ./...", "go test ./...", true},
		{"cd prefix dropped", "git merge upstream/main-v2 --ff-only",
			"cd /repo && git merge upstream/main-v2 --ff-only", true},
		{"flag drift inside compound", "rm -v scripts/test_lines.txt && ls scripts/test_lines.txt 2>&1",
			"rm -v scripts/test_lines.txt && ls -la scripts/test_lines.txt 2>&1 || true", true},
		{"quote style drift", `test -f test-tools.md && echo 'still exists' || echo 'deleted'`,
			`test -f test-tools.md && echo "still exists" || echo "deleted"`, true},
		{"pipe tail dropped", "go test ./internal/tool/... -count=1 -timeout 60s",
			"cd /f/Reasonix && go test ./internal/tool/... -count=1 -timeout 60s 2>&1 | tail -10", true},
		{"whitespace collapse", "go  test   ./...", "go test ./...", true},
		{"different command rejected", "go test ./...", "go vet ./...", false},
		{"extra cited token rejected", "go test ./a/... ./b/...", "go test ./a/...", false},
		{"bare token needs exact", "ls", "ls -la scripts", false},
		{"bare token exact ok", "ls", "ls", true},
		{"empty cited", "", "go test ./...", false},
		{"comment lines ignored", "shuf -i 1-30 -n 10",
			"# pick lines to delete\nshuf -i 1-30 -n 10 | sort -rn", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CommandMatches(tc.cited, tc.ran); got != tc.want {
				t.Fatalf("CommandMatches(%q, %q) = %v, want %v", tc.cited, tc.ran, got, tc.want)
			}
		})
	}
}
