package shellsafe

import "testing"

func TestCommandIsReadOnly(t *testing.T) {
	readOnly := []string{
		// git read-only subcommands (the #5341 set, beyond status/diff/log/show).
		"git status", "git diff HEAD", "git log --oneline", "git show",
		"git rev-parse HEAD", "git describe --tags", "git reflog",
		"git for-each-ref", "git cat-file -p HEAD", "git ls-tree HEAD",
		"git rev-list --count HEAD", "git shortlog", "git name-rev HEAD",
		`git log "2>/dev/null"`, `git log 2\>/dev/null`,
		// general read-only commands.
		"ls -la", "cat go.mod", "grep -r foo .", "pwd", "head -n5 x", "stat x", "du -sh .",
		`grep 'a|b' file`, `printf "%s\n" "a && b"`,
		// tooling probes.
		"go version", "go env", "go list ./...", "go doc fmt",
		"npm view react version", "npm outdated", "cargo check",
		"docker ps", "docker images", "kubectl get pods",
		"node -v", "node --version", "python --version", "python3 --version",
		// PowerShell permission-safe inspection commands.
		`Get-Process -Name mongod`, `Get-ChildItem -Path .`,
		`Get-NetTCPConnection -LocalPort 6379`, `Resolve-Path .`,
		// Narrow, recursively proven read-only command substitution.
		`basename "$(pwd)"`, `dirname "$(realpath .)"`,
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
		"git status > out.txt", "ls; rm x", "git log `whoami`", "echo $HOME",
		`basename "$(touch out)"`, `basename "$(pwd; touch out)"`,
		`basename "$(date --set tomorrow)"`, `basename "$(find . -delete)"`,
		`basename $(pwd)`, `basename "$HOME"`, `find . "$(printf -- -delete)"`,
		// unknown command.
		"frobnicate --all",
		// Network probes do not write the workspace, but they are not safe to
		// auto-allow through the permission-layer read-only classifier.
		`Test-NetConnection -ComputerName example.com -Port 443`,
		// PowerShell mutators stay fail-closed.
		`Start-Process mongod`, `Stop-Process -Name mongod`,
		`Set-Content style.css bad`, `Remove-Item style.css`,
	}
	for _, c := range notReadOnly {
		if _, _, ok := CommandIsReadOnly(c); ok {
			t.Errorf("CommandIsReadOnly(%q) = true, want false", c)
		}
	}
}

func TestCommandIsWorkspaceNonMutatingKeepsNetworkProbeOutOfPermissionReaders(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{command: `Test-NetConnection -ComputerName example.com -Port 443`, want: true},
		{command: `Get-Process -Name mongod`, want: true},
		{command: `Test-NetConnection example.com; Set-Content out.txt bad`},
		{command: `Set-Content out.txt bad`},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			_, _, got := CommandIsWorkspaceNonMutating(tt.command)
			if got != tt.want {
				t.Fatalf("CommandIsWorkspaceNonMutating(%q) = %t, want %t", tt.command, got, tt.want)
			}
		})
	}
}

func TestContainsShellSyntax(t *testing.T) {
	for _, c := range []string{"a && b", "a || b", "a | b", "a; b", "a > f", "a < f", "a & ", "$(x)", "`x`", "a\nb"} {
		if !ContainsShellSyntax(c) {
			t.Errorf("ContainsShellSyntax(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"git status", "ls -la", "grep foo bar.go", `grep 'a|b' file`, `echo "a && b"`} {
		if ContainsShellSyntax(c) {
			t.Errorf("ContainsShellSyntax(%q) = true, want false", c)
		}
	}
}
