package shellsafe

import "testing"

func TestCommandIsReadOnly(t *testing.T) {
	readOnly := []string{
		// git read-only subcommands (the #5341 set, beyond status/diff/log/show).
		"git status", "git diff HEAD", "git log --oneline", "git show",
		"git rev-parse HEAD", "git describe --tags", "git reflog",
		"git for-each-ref", "git cat-file -p HEAD", "git ls-tree HEAD",
		"git rev-list --count HEAD", "git shortlog", "git name-rev HEAD",
		// general read-only commands.
		"ls -la", "cat go.mod", "grep -r foo .", "pwd", "head -n5 x", "stat x", "du -sh .",
		// tooling probes.
		"go version", "go env", "go list ./...", "go doc fmt",
		"npm view react version", "npm outdated", "cargo check",
		"docker ps", "docker images", "kubectl get pods",
		"node -v", "node --version", "python --version", "python3 --version",
	}
	for _, c := range readOnly {
		if _, _, ok := CommandIsReadOnly(c); !ok {
			t.Errorf("CommandIsReadOnly(%q) = false, want true", c)
		}
	}

	notReadOnly := []string{
		// write-capable commands / subcommands.
		"rm -rf /", "git push", "git commit -m x", "git checkout main",
		"git reset --hard", "git branch -d feature", "git remote add o url",
		"go build ./...", "go test ./...", "npm install", "docker rm x",
		"kubectl apply -f x.yaml", "mv a b", "chmod 777 x",
		// shell syntax can smuggle a write past a read-only base word.
		"git status && rm -rf /", "cat a | tee b", "echo $(rm x)",
		"git status > out.txt", "ls; rm x", "git log `whoami`",
		// unknown command.
		"frobnicate --all",
	}
	for _, c := range notReadOnly {
		if _, _, ok := CommandIsReadOnly(c); ok {
			t.Errorf("CommandIsReadOnly(%q) = true, want false", c)
		}
	}
}

func TestContainsShellSyntax(t *testing.T) {
	for _, c := range []string{"a && b", "a || b", "a | b", "a; b", "a > f", "a < f", "a & ", "$(x)", "`x`", "a\nb"} {
		if !ContainsShellSyntax(c) {
			t.Errorf("ContainsShellSyntax(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"git status", "ls -la", "grep foo bar.go"} {
		if ContainsShellSyntax(c) {
			t.Errorf("ContainsShellSyntax(%q) = true, want false", c)
		}
	}
}
