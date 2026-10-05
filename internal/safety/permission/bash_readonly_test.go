package permission

import (
	"encoding/json"
	"testing"
)

func TestIsReadOnlyBashSubject(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		// Read-only commands
		{"ls", true},
		{"ls /tmp", true},
		{"cat main.go", true},
		{"head -n 5 file.txt", true},
		{"find . -name '*.go'", true},
		{"grep TODO *.go", true},
		{"grep 'a|b' file", true},
		{"rg pattern", true},
		{"echo hello", true},
		{"pwd", true},
		{"cd /tmp/project", true},
		{"whoami", true},
		{"wc -l file.txt", true},
		{"stat main.go", true},
		{"du -sh .", true},
		{"diff a.go b.go", true},
		{"printenv PATH", true},
		{"Test-NetConnection -ComputerName localhost -Port 27017", false},
		{"Get-Process -Name mongod", true},
		{"Get-ChildItem -Path .", true},
		{"Get-NetTCPConnection -LocalPort 6379", true},
		{`basename "$(pwd)"`, true},

		// Git read-only
		{"git log", true},
		{"git status", true},
		{"git diff", true},
		{"git show HEAD", true},
		{"git blame main.go", true},
		{"git log 2>/dev/null", true},
		{"git log >/dev/null", true},
		{"git log >$null", true},
		{"git log >NUL", true},
		{"git log 2>&1", true},
		{"git log &>/dev/null", true},
		{"git remote", false},
		{"git remote add origin git@example.com:x/y", false},
		{"git config --global user.name Xinwei", false},
		{"git stash", false},
		{"git stash push", false},
		{"git archive --output repo.tar HEAD", false},
		{"git bundle create repo.bundle HEAD", false},
		{"git diff --output changes.patch", false},
		{"git show --output=changes.patch HEAD", false},
		{"git diff --output changes.patch 2>/dev/null", false},
		{"git log > changes.patch", false},
		{"git log >$nullish", false},
		{"git log >nul.txt", false},
		{"git log < /dev/null", false},

		// Go read-only
		{"go vet ./...", true},
		{"go doc fmt", true},
		{"go list ./...", true},
		{"go env -w GOPROXY=https://proxy.golang.org,direct", false},
		{"go env -u GOPROXY", false},

		// Not read-only
		{"rm file.txt", false},
		{"echo $HOME", false},
		{"rm -rf /", false},
		{"env rm -rf /", false},
		{"git commit -m 'msg'", false},
		{"git branch", false},
		{"git branch feature/new", false},
		{"git push", false},
		{"git push --force", false},
		{"cd /tmp && rm file.txt", false},
		{"go build ./...", false},
		{"go fmt ./...", false},
		{"go test ./...", false},
		{"ls; rm file.txt", false},
		{"git status && rm file.txt", false},
		{"cat main.go | tee copy.go", false},
		{"cat file > output.txt", false},
		{"git diff > changes.patch", false},
		{"git diff >> changes.patch", false},
		{"cat < input.txt", false},
		{"diff <(sort a.txt) <(sort b.txt)", false},
		{"cat >(tee output.txt)", false},
		{"ls $(touch output.txt)", false},
		{`basename "$(touch output.txt)"`, false},
		{`basename $(pwd)`, false},
		{"ls `touch output.txt`", false},
		{"ls || rm file.txt", false},
		{"ls & rm file.txt", false},
		{"find . -name '*.go' | xargs gofmt -w", false},
		{"find . -name '*.go' -exec rm {} \\;", false},
		{"find . -name '*.go' -ok rm {} \\;", false},
		{"find . -name '*.go' -okdir rm {} \\;", false},
		{"find . -name '*.tmp' -delete", false},
		{"find . -name '*.go' -fls files.txt", false},
		{"find . -name '*.go' -fprint files.txt", false},
		{"find . -name '*.go' -fprint0 files.txt", false},
		{"find . -name '*.go' -fprintf files.txt '%p\\n'", false},
		{"awk '{print $1}' file.txt", false},
		{"awk 'BEGIN{system(\"touch /tmp/reasonix-awk-test\")}'", false},
		{"awk 'BEGIN{print \"evil\" > \"/tmp/reasonix-awk-test\"}'", false},
		{"sed 's/a/b/' file.txt", false},
		{"sed -e 's/.*/touch \\/tmp\\/reasonix-sed-test/e' file.txt", false},
		{"sed -i 's/a/b/' file.txt", false},
		{"sed -i.bak 's/a/b/' file.txt", false},
		{"sed -ibak 's/a/b/' file.txt", false},
		{"sed --in-place 's/a/b/' file.txt", false},
		{"sort -o sorted.txt input.txt", false},
		{"sort -osorted.txt input.txt", false},
		{"sort --output=sorted.txt input.txt", false},
		{"tee out.txt", false},
		{"xargs rm", false},
		{"make build", false},
		{"curl https://example.com", false},
		{"npm install", false},
		{"chmod 777 file", false},
		{"Start-Process mongod", false},
		{"Stop-Process -Name mongod", false},
		{"Set-Content style.css bad", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := isReadOnlyBashSubject(tt.cmd); got != tt.want {
				t.Errorf("isReadOnlyBashSubject(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestBashCommandIsReadOnlyRejectsProcessLifecycleFlags(t *testing.T) {
	tests := []struct {
		name string
		args string
		want bool
	}{
		{name: "foreground reader", args: `{"command":"git status"}`, want: true},
		{name: "background reader", args: `{"command":"git status","run_in_background":true}`},
		{name: "preserved reader", args: `{"command":"git status","preserve_background_processes":true}`},
		{name: "writer", args: `{"command":"rm -rf build"}`},
		{name: "missing command", args: `{}`},
		{name: "malformed", args: `{"command":`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BashCommandIsReadOnly(json.RawMessage(tc.args)); got != tc.want {
				t.Fatalf("BashCommandIsReadOnly(%s) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestBashDangerWarning(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"ls", ""},
		{"cat main.go", ""},
		{"rm -rf /tmp/build", "recursive delete"},
		{"rm -r old_files", "recursive delete"},
		{"git push --force origin main", "force push"},
		{"git push -f", "force push"},
		{"git reset --hard HEAD~1", "hard reset"},
		{"chmod 777 script.sh", "world-writable"},
		{"sudo make install", "superuser"},
		{"git clean -fd", "force clean"},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := BashDangerWarning(tt.cmd); got != tt.want {
				t.Errorf("BashDangerWarning(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

// TestBashPipelineReadOnly pins where a pipeline sits: a stage that only reads
// keeps the whole call read-only, and one writer anywhere ends it. Pipelines
// used to fall between the single-command classifier and the compound one, so
// every `… | head` counted as a write.
func TestBashPipelineReadOnly(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{`grep -rn pattern binding/*.go context.go | head -40`, true},
		{`ls -la | head -20`, true},
		{`cat file.go | wc -l`, true},
		{`grep -rn x . && ls -la`, true},
		{`grep -rn x . | tee out.txt`, false},
		{`find . -name "*.go" | xargs grep pattern`, false},
		{`ls | rm -rf x`, false},
		{`cat f | python3 -c "import os"`, false},
		{`grep x . && rm -rf build`, false},
		{`ls | sed -i s/a/b/ f`, false},
		{`cat f | tee >(sh)`, false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"command": tt.cmd})
			if err != nil {
				t.Fatal(err)
			}
			if got := BashCommandIsReadOnly(args); got != tt.want {
				t.Errorf("BashCommandIsReadOnly(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// `;` chains commands the way `&&` and `|` do, but only the operators marked a
// statement compound, so `a; b` fell between the single-command classifier and
// the compound one and every chain counted as a write.
func TestSemicolonChainIsReadOnlyWhenEveryLeafIs(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want bool
	}{
		{`echo hi; pwd`, true},
		{`printenv GOROOT; printenv GOTOOLCHAIN`, true},
		{`which -a go; go env GOROOT`, true},
		{`cat a.txt; ls`, true},
		{`cd /tmp; ls`, true},

		{`ls; rm -rf /tmp/x`, false},           // a writer leaf
		{`echo hi; go build ./...`, false},     // go build writes
		{`pwd; python3 -c "import os"`, false}, // inline code
		{`ls > out.txt; pwd`, false},           // the redirect creates a file
		{`ls; $CMD`, false},                    // the program itself is dynamic
	} {
		args, err := json.Marshal(map[string]string{"command": tc.cmd})
		if err != nil {
			t.Fatal(err)
		}
		if got := BashCommandIsReadOnly(args); got != tc.want {
			t.Errorf("%q read-only = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}
