package jobs

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const jobsHolderEnv = "REASONIX_JOBS_HOLDER"

func TestKilledKernelsJobTempDirIsCollectedByNextManager(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), jobsHolderEnv+"=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("holder did not report its temp root: %v", err)
	}
	leaked := strings.TrimSpace(line)

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if _, err := os.Stat(leaked); err != nil {
		t.Fatalf("a killed kernel cannot clean up, yet %s vanished: %v", leaked, err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(leaked, old, old); err != nil {
		t.Fatal(err)
	}

	m := NewManager(nil)
	defer m.Close()
	if _, err := os.Stat(leaked); !os.IsNotExist(err) {
		t.Fatalf("next manager left the killed kernel's temp root behind: %v", err)
	}
	if _, err := os.Stat(m.scratch.Path()); err != nil {
		t.Fatalf("sweep removed the live manager's own root: %v", err)
	}
	m.Close()
	if _, err := os.Stat(m.scratch.Path()); !os.IsNotExist(err) {
		t.Fatalf("Close left the temp root behind: %v", err)
	}
}
