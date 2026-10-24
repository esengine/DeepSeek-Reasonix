package executionpresence

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPresenceProcessHelper(t *testing.T) {
	if os.Getenv("REASONIX_PRESENCE_HELPER") != "1" {
		return
	}
	input := bufio.NewReader(os.Stdin)
	if line, err := input.ReadString('\n'); err != nil || line != "start\n" {
		t.Fatalf("missing start barrier: %q %v", line, err)
	}
	s := storeFor(t, os.Getenv("REASONIX_PRESENCE_ROOT"), os.Getenv("REASONIX_PRESENCE_STATE"))
	r, err := s.Reserve(t.Context(), Request{Strategy: Auto, Writer: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if err := json.NewEncoder(os.Stdout).Encode(r.Record()); err != nil {
		t.Fatal(err)
	}
	_, _ = input.ReadString('\n')
}

func TestTwoProcessesChooseAtMostOneSharedWriterAndRecoverCrash(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	type child struct {
		cmd   *exec.Cmd
		start func()
		ready <-chan Record
	}
	var children []child
	for range 2 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPresenceProcessHelper$")
		cmd.Env = append(os.Environ(), "REASONIX_PRESENCE_HELPER=1", "REASONIX_PRESENCE_ROOT="+root, "REASONIX_PRESENCE_STATE="+state)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
		ready := make(chan Record, 1)
		go func() {
			var record Record
			_ = json.NewDecoder(stdout).Decode(&record)
			ready <- record
		}()
		children = append(children, child{cmd: cmd, start: func() { _, _ = stdin.Write([]byte("start\n")) }, ready: ready})
	}
	for _, child := range children {
		child.start()
	}
	shared, isolated := 0, 0
	var sharedProcess *exec.Cmd
	for _, child := range children {
		select {
		case record := <-child.ready:
			switch record.Selected {
			case Shared:
				shared++
				sharedProcess = child.cmd
			case Isolated:
				isolated++
			default:
				t.Fatalf("child did not reserve a known environment: %+v", record)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("presence child did not reach its barrier")
		}
	}
	if shared != 1 || isolated != 1 {
		t.Fatalf("simultaneous first execution selected shared=%d isolated=%d", shared, isolated)
	}
	if err := sharedProcess.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = sharedProcess.Wait()
	s := storeFor(t, root, state)
	if got := reserve(t, s, Request{Strategy: Auto, Writer: true}).Record().Selected; got != Shared {
		t.Fatalf("dead writer presence prevented source reuse: %s", got)
	}
}
