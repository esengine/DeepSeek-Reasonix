package shellsafe

import (
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

var syntaxBases = []string{
	"cat a.txt", "ls", "ls -l sub", "echo hi", "head -n 1 a.txt", "wc -l b.txt", "grep alpha a.txt", "pwd", "stat a.txt", "tail b.txt", "printf x", "basename a.txt",
}

// syntaxWrappers puts shell syntax around, or inside, a command that is a read
// on its own. %s is the command. Every one either writes, runs something else,
// or makes the classifier's reading of the words unsafe to trust.
var syntaxWrappers = []string{
	"%s > out.txt", "%s >> new.txt", "%s 2> err.txt", "%s &> both.txt", "%s >| a.txt", "%s > /dev/null", "%s 2>&1", "%s <&-",
	"%s | tee tee.txt", "%s | cat", "%s | sh", "%s; touch semi.txt", "%s && touch and.txt", "%s || touch or.txt", "%s & touch bg.txt; wait",
	"%s\ntouch nl.txt", "(%s; touch sub.txt)", "{ %s; touch grp.txt; }", "! %s", "if %s; then touch if.txt; fi", "for i in 1; do %s; touch loop.txt; done",
	"echo $(touch sub1.txt)", "echo `touch sub2.txt`", "echo $(%s)", "echo \"$(touch sub3.txt)\"", "%s <(touch proc.txt)", "%s $(touch arg.txt)",
	"cat <<EOF > here.txt\nx\nEOF", "%s <<EOF\nx\nEOF", "%s <<< word", "X=1 %s", "FOO=$(touch env.txt) %s", "%s ${X:=$(touch exp.txt)}", "%s {a,b}.txt",
	"%s *.txt", "%s ~", "%s $HOME", "env %s", "command %s", "exec %s", "time %s", "nohup %s", "xargs touch < a.txt", "eval '%s; touch ev.txt'", "sh -c '%s; touch shc.txt'",
	"cd sub && %s", "%s;", "%s &", "%s # comment", "%s\\\n a.txt",
}

// quoteWords rewrites a command so its words arrive by a different spelling:
// quoted, escaped or split. The shell reads them the same way; a classifier
// that reads them differently has been handed a way round its tables.
func quoteWords(rng *rand.Rand, command string) string {
	words := strings.Fields(command)
	for i, w := range words {
		switch rng.Intn(6) {
		case 0:
			words[i] = "'" + w + "'"
		case 1:
			words[i] = `"` + w + `"`
		case 2:
			if len(w) > 1 {
				words[i] = w[:1] + "'" + w[1:] + "'"
			}
		case 3:
			words[i] = `\` + w
		case 4:
			if len(w) > 2 {
				words[i] = w[:1] + `""` + w[1:]
			}
		}
	}
	return strings.Join(words, " ")
}

func TestEverySyntaxWrappedCommandTheClassifierCallsAReadLeavesTheDirectoryAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a process per generated command")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the generated commands are POSIX shell commands")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	rng := rand.New(rand.NewSource(20261018))
	scratch := testenv.TempDir(t)
	home := testenv.TempDir(t)
	classified, bare := 0, 0
	seen := map[string]bool{}
	for range 4000 {
		base := syntaxBases[rng.Intn(len(syntaxBases))]
		if rng.Intn(2) == 0 {
			base = quoteWords(rng, base)
		}
		command := base
		if rng.Intn(8) != 0 {
			wrapper := syntaxWrappers[rng.Intn(len(syntaxWrappers))]
			command = strings.ReplaceAll(wrapper, "%s", base)
		}
		if seen[command] {
			continue
		}
		seen[command] = true
		b, sub, fields, ok := ClassifyReadOnlyCommand(command)
		if !ok || ArgsMakeReadOnlyCommandWrite(b, sub, fields) {
			continue
		}
		classified++
		if command == base {
			bare++
		}
		if err := os.RemoveAll(scratch); err != nil {
			t.Fatal(err)
		}
		seedScratch(t, scratch)
		before := snapshotDir(t, scratch)
		cmd := exec.Command(sh, "-c", command)
		cmd.Dir = scratch
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
		done := make(chan struct{})
		go func() {
			_ = cmd.Run()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if after := snapshotDir(t, scratch); after != before {
			t.Errorf("classified as a read but changed the directory: %q\nbefore:\n%s\nafter:\n%s", command, before, after)
		}
	}
	if bare < 20 {
		t.Fatalf("only %d plain commands were classified as reads (of %d); the generator no longer reaches the classifier's table", bare, classified)
	}
	t.Logf("%d commands classified as reads (%d without a wrapper)", classified, bare)
}
