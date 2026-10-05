package shellsafe

import "testing"

// Each of these runs another program or writes, spelled the ways the program
// itself accepts; the reads beside them must stay reads.
func TestReadOnlyCommandsRefuseTheirEscapes(t *testing.T) {
	escapes := []string{
		"git grep -O'touch PWNED; true' hello", "git -C . grep -Osh x", "git grep -nO x",
		"git grep --open-files-in-pager=sh x", "git grep --open-files x", "git grep --open x",
		"git log --outp=f", "git show --output f", "git reflog expire --expire=now --all", "git reflog delete HEAD@{1}",
		"rg --pre sh x", "rg --pre=./x y",
		"man -P cat ls", "less +!sh x", "less -o log x",
		"sort --compress-program=sh f", "sort --comp=sh f", "sort -no out f", "sort --outp=out f",
		"go vet -vettool=./x ./...", "go list -toolexec=./x -export ./...", "go vet --toolexec ./x", "go list -exec ./x",
		"cargo check", "cargo doc",
		"uniq a b", "uniq -f 1 a b",
		"file -C -m x", "file --compile -m x", "file -bC x",
		"date -s 2020-01-01", "date --set=tomorrow", "date -us now",
		"hostname evil", "hostname -F f",
		"npm audit fix",
		"kubectl get pods --kubeconfig=./k", "kubectl get pods --kubeconfig ./k",
		"gofmt -cpuprofile out -l .", "gofmt -w=true a.go", "gofmt --w=true a.go",
		"go env -w=true GOFLAGS=-toolexec=./x", "go env --w GOFLAGS=x", "go env -u=true GOFLAGS",
		"rg --hostname-bin=./e --hyperlink-format=default x", "rg --hostname-bin ./e x",
		"info -o out ls", "git reflog write x", "git reflog main",
		"go list -mod=mod ./...", "go doc -http", "env -S'sh -c x'", "env --split-string=sh",
	}
	for _, c := range escapes {
		if base, sub, fields, ok := ClassifyReadOnlyCommand(c); ok && !ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
			t.Errorf("%q reads as a read", c)
		}
	}
	reads := []string{
		"git grep -n foo", "git grep -e x -- a.go", "git log --oneline", "git reflog", "git reflog show",
		"rg --pre-glob '*.gz' x", "rg -n foo",
		"sort -n f", "sort -r -k2 f", "uniq a", "uniq -c", "uniq -f 1 a",
		"file -b x", "file -m magic x", "date", "date -d '-2 days'", "date -u +%s",
		"hostname", "hostname -f", "npm audit", "npm audit --json",
		"kubectl get pods -n x", "go vet ./...", "go list -json ./...", "gofmt -l .",
		"more README.md", "cargo search serde", "date -Iseconds", "git reflog show", "git reflog --all",
		"go env GOFLAGS", "env", "env -i",
	}
	for _, c := range reads {
		base, sub, fields, ok := ClassifyReadOnlyCommand(c)
		if !ok || ArgsMakeReadOnlyCommandWrite(base, sub, fields) {
			t.Errorf("%q no longer reads as a read (classified %v)", c, ok)
		}
	}
}
