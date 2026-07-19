//go:build windows

package proc

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A launcher (cmd.exe) that spawns a long-lived grandchild (ping) which inherits
// the stdout pipe: killing only the direct child leaves the grandchild holding
// the pipe, so cmd.Wait blocks until the grandchild exits. KillTree must take
// the whole tree down so Wait returns promptly.
func TestKillTreeUnblocksWaitOnSurvivingGrandchild(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	time.Sleep(500 * time.Millisecond) // let cmd.exe exec the ping grandchild

	KillTree(cmd)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("cmd.Wait blocked after KillTree — grandchild survived holding the pipe")
	}
}

func TestKillTreeSkipsPIDTaskkillAfterProcessWaitReleasedHandle(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	HideWindow(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	processWaitFinished := make(chan struct{})
	releaseOuterWait := make(chan struct{})
	outerWaitDone := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		close(processWaitFinished)
		<-releaseOuterWait
		outerWaitDone <- err
	}()
	<-processWaitFinished

	taskkillCalled := false
	killTreeWithTaskkill(cmd, func(int) { taskkillCalled = true })
	if taskkillCalled {
		close(releaseOuterWait)
		t.Fatal("PID-based taskkill ran after Process.Wait released the handle while the outer wait remained blocked")
	}
	close(releaseOuterWait)
	if err := <-outerWaitDone; err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestJoblessRetryKillsRecordedDescendantAfterRootHandleRelease(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTreeTrackerDetachedGrandchildHelper$")
	cmd.Env = replaceTreeTrackerHelperMode(os.Environ(), "root")
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
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			KillTree(cmd)
			_ = cmd.Wait()
		}
	})
	tracker := TrackTree(cmd)
	if tracker == nil {
		t.Fatal("TreeTracker could not pin the root process identity")
	}
	t.Cleanup(tracker.Stop)

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	descendantPID, err := strconv.ParseUint(strings.TrimSpace(line), 10, 32)
	if err != nil || descendantPID == 0 {
		t.Fatalf("parse descendant PID %q: %v", line, err)
	}
	descendant, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_TERMINATE,
		false,
		uint32(descendantPID),
	)
	if err != nil {
		t.Fatalf("open descendant %d: %v", descendantPID, err)
	}
	t.Cleanup(func() {
		_ = windows.TerminateProcess(descendant, 1)
		_ = windows.CloseHandle(descendant)
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		tracker.record()
		found := false
		for _, record := range tracker.snapshot() {
			if record.pid == uint32(descendantPID) {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TreeTracker did not record descendant %d", descendantPID)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := io.WriteString(stdin, "exit\n"); err != nil {
		t.Fatalf("release root helper: %v", err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for root helper: %v", err)
	}

	// This is the retry window that used to be unsafe: Process.Wait has
	// released the root handle, so KillTracked must skip taskkill /PID. The
	// immutable tracker still owns enough identity to reap the descendant.
	KillTracked(cmd, 0)
	if killed := tracker.Kill(); killed == 0 {
		t.Fatal("identity-checked retry did not terminate the recorded descendant")
	}
	result, err := windows.WaitForSingleObject(descendant, uint32((5*time.Second)/time.Millisecond))
	if err != nil {
		t.Fatalf("wait for recorded descendant: %v", err)
	}
	if result != windows.WAIT_OBJECT_0 {
		t.Fatalf("recorded descendant survived identity-checked retry (wait result %d)", result)
	}
}

func TestJoblessFinishTrackingKillsRecordedDescendantAfterNormalRootExit(t *testing.T) {
	cmd, tracker, stdin, descendant := startJoblessTrackedDetachedGrandchild(t)
	if _, err := io.WriteString(stdin, "exit\n"); err != nil {
		t.Fatalf("release root helper: %v", err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait for root helper: %v", err)
	}

	tracked := &TrackedCommand{cmd: cmd, tree: tracker}
	tracked.finishTracking()
	if tracked.Diagnostics().TreeKillAttempts == 0 {
		t.Fatal("normal root exit did not terminate any recorded descendant identities")
	}
	result, err := windows.WaitForSingleObject(descendant, uint32((5*time.Second)/time.Millisecond))
	if err != nil {
		t.Fatalf("wait for recorded descendant: %v", err)
	}
	if result != windows.WAIT_OBJECT_0 {
		t.Fatalf("recorded descendant survived jobless finishTracking (wait result %d)", result)
	}
}

func startJoblessTrackedDetachedGrandchild(
	t *testing.T,
) (*exec.Cmd, *TreeTracker, io.WriteCloser, windows.Handle) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTreeTrackerDetachedGrandchildHelper$")
	cmd.Env = replaceTreeTrackerHelperMode(os.Environ(), "root")
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
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			KillTree(cmd)
			_ = cmd.Wait()
		}
	})

	tracker := TrackTree(cmd)
	if tracker == nil {
		t.Fatal("TreeTracker could not pin the root process identity")
	}
	t.Cleanup(tracker.Stop)
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read descendant PID: %v", err)
	}
	descendantPID, err := strconv.ParseUint(strings.TrimSpace(line), 10, 32)
	if err != nil || descendantPID == 0 {
		t.Fatalf("parse descendant PID %q: %v", line, err)
	}
	descendant, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_TERMINATE,
		false,
		uint32(descendantPID),
	)
	if err != nil {
		t.Fatalf("open descendant %d: %v", descendantPID, err)
	}
	t.Cleanup(func() {
		_ = windows.TerminateProcess(descendant, 1)
		_ = windows.CloseHandle(descendant)
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		tracker.record()
		for _, record := range tracker.snapshot() {
			if record.pid == uint32(descendantPID) {
				return cmd, tracker, stdin, descendant
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("TreeTracker did not record descendant %d", descendantPID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTreeTrackerDetachedGrandchildHelper(t *testing.T) {
	switch os.Getenv("GO_WANT_TREE_TRACKER_HELPER") {
	case "":
		return
	case "child":
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	case "root":
		child := exec.Command(os.Args[0], "-test.run=^TestTreeTrackerDetachedGrandchildHelper$")
		child.Env = replaceTreeTrackerHelperMode(os.Environ(), "child")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.WriteString(strconv.Itoa(child.Process.Pid) + "\n")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(0)
	default:
		os.Exit(3)
	}
}

func replaceTreeTrackerHelperMode(env []string, mode string) []string {
	const key = "GO_WANT_TREE_TRACKER_HELPER="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), key) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, key+mode)
}

// StartTracked must create a Job Object for the started process, and KillTracked
// must take the whole tracked tree down, including descendants a plain taskkill
// /T would miss.
func TestKillTrackedReapsTrackedTree(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	job, err := StartTracked(cmd)
	if err != nil {
		t.Fatalf("StartTracked: %v", err)
	}
	if job == 0 {
		t.Fatal("StartTracked returned 0 — job object not created")
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	time.Sleep(500 * time.Millisecond) // let cmd.exe exec the ping grandchild into the job

	KillTracked(cmd, job)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("cmd.Wait blocked after KillTracked — tracked tree survived")
	}
}

// StartTracked creates the child suspended to win the job-assignment race, so it
// must resume it or the process hangs forever. A child that exits with a known
// code proves the resume fired: Wait returns that code instead of timing out.
func TestStartTrackedResumesSuspendedChild(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "7")
	HideWindow(cmd)
	if _, err := StartTracked(cmd); err != nil {
		t.Fatalf("StartTracked: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 7 {
			t.Fatalf("exit = %v, want exit status 7", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("cmd.Wait blocked — StartTracked left the child suspended")
	}
}

func TestStartTrackedRequiredFailsClosedBeforeChildRuns(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-ran")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStartTrackedRequiredHelper$")
	cmd.Env = append(os.Environ(),
		"GO_WANT_START_TRACKED_REQUIRED_HELPER=1",
		"START_TRACKED_REQUIRED_MARKER="+marker,
	)

	job, err := startTrackedWithJobAssigner(cmd, true, func(*exec.Cmd) uintptr { return 0 })
	if !errors.Is(err, ErrProcessTrackingUnavailable) {
		t.Fatalf("StartTrackedRequired failure = %v, want ErrProcessTrackingUnavailable", err)
	}
	if job != 0 {
		t.Fatalf("StartTrackedRequired job = %d, want 0", job)
	}
	if state := cmd.ProcessState; state == nil || !state.Exited() {
		t.Fatalf("untracked suspended child was not reaped: %#v", state)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("untracked child ran before fail-closed termination: %v", statErr)
	}
}

func TestResumeProcessPropagatesNativeOperationFailures(t *testing.T) {
	snapshotFailure := errors.New("forced snapshot failure")
	openFailure := errors.New("forced open failure")
	resumeFailure := errors.New("forced resume failure")

	tests := []struct {
		name string
		want error
		ops  resumeProcessOperations
	}{
		{
			name: "snapshot",
			want: snapshotFailure,
			ops: resumeProcessOperations{
				createSnapshot: func() (windows.Handle, error) { return 0, snapshotFailure },
			},
		},
		{
			name: "open thread",
			want: openFailure,
			ops: fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
				return 0, openFailure
			}, func(windows.Handle) (uint32, error) {
				return 0, nil
			}),
		},
		{
			name: "resume thread",
			want: resumeFailure,
			ops: fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
				return windows.Handle(2), nil
			}, func(windows.Handle) (uint32, error) {
				return 0, resumeFailure
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := resumeProcessWithOperations(41, test.ops)
			if !errors.Is(err, test.want) {
				t.Fatalf("resumeProcessWithOperations error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestResumeProcessFailsClosedWhenThreadRemainsSuspended(t *testing.T) {
	threadClosed := false
	ops := fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
		return windows.Handle(2), nil
	}, func(windows.Handle) (uint32, error) {
		return 2, nil
	})
	ops.closeHandle = func(handle windows.Handle) error {
		if handle == windows.Handle(2) {
			threadClosed = true
		}
		return nil
	}

	if err := resumeProcessWithOperations(41, ops); err == nil {
		t.Fatal("ResumeThread previous suspend count 2 was accepted as runnable")
	}
	if !threadClosed {
		t.Fatal("thread handle was not closed after the still-suspended result")
	}
}

func TestResumeProcessTreatsZeroPreviousCountAsRunningCommitPoint(t *testing.T) {
	ops := fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
		return windows.Handle(2), nil
	}, func(windows.Handle) (uint32, error) {
		return 0, nil
	})

	if err := resumeProcessWithOperations(41, ops); err != nil {
		t.Fatalf("already-running ResumeThread result triggered suspended rollback: %v", err)
	}
}

func TestResumeProcessRejectsReusedThreadID(t *testing.T) {
	resumeCalled := false
	threadClosed := false
	ops := fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
		return windows.Handle(2), nil
	}, func(windows.Handle) (uint32, error) {
		resumeCalled = true
		return 1, nil
	})
	ops.threadOwner = func(windows.Handle) (uint32, error) { return 84, nil }
	ops.closeHandle = func(handle windows.Handle) error {
		if handle == windows.Handle(2) {
			threadClosed = true
		}
		return nil
	}

	err := resumeProcessWithOperations(41, ops)
	if err == nil || !strings.Contains(err.Error(), "owner changed from 41 to 84") {
		t.Fatalf("reused thread ID error = %v", err)
	}
	if resumeCalled {
		t.Fatal("ResumeThread ran after OpenThread resolved a reused thread ID")
	}
	if !threadClosed {
		t.Fatal("reused thread handle was not closed")
	}
}

func TestResumeProcessCompletesEnumerationBeforeResume(t *testing.T) {
	enumerationFailure := errors.New("forced post-discovery enumeration failure")
	resumeCalled := false
	snapshotClosed := false
	threadClosed := false
	ops := fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
		return windows.Handle(2), nil
	}, func(windows.Handle) (uint32, error) {
		resumeCalled = true
		return 1, nil
	})
	ops.threadNext = func(windows.Handle, *windows.ThreadEntry32) error {
		return enumerationFailure
	}
	ops.closeHandle = func(handle windows.Handle) error {
		switch handle {
		case windows.Handle(1):
			snapshotClosed = true
		case windows.Handle(2):
			threadClosed = true
		}
		return nil
	}

	err := resumeProcessWithOperations(41, ops)
	if !errors.Is(err, enumerationFailure) {
		t.Fatalf("resumeProcessWithOperations error = %v, want %v", err, enumerationFailure)
	}
	if resumeCalled {
		t.Fatal("ResumeThread ran before thread enumeration completed")
	}
	if !snapshotClosed || !threadClosed {
		t.Fatalf("pre-resume handles were not closed: snapshot=%t thread=%t", snapshotClosed, threadClosed)
	}
}

func TestResumeProcessClosesSnapshotBeforeResume(t *testing.T) {
	snapshotCloseFailure := errors.New("forced snapshot close failure")
	resumeCalled := false
	threadClosed := false
	ops := fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
		return windows.Handle(2), nil
	}, func(windows.Handle) (uint32, error) {
		resumeCalled = true
		return 1, nil
	})
	ops.closeHandle = func(handle windows.Handle) error {
		if handle == windows.Handle(1) {
			return snapshotCloseFailure
		}
		if handle == windows.Handle(2) {
			threadClosed = true
		}
		return nil
	}

	err := resumeProcessWithOperations(41, ops)
	if !errors.Is(err, snapshotCloseFailure) {
		t.Fatalf("resumeProcessWithOperations error = %v, want %v", err, snapshotCloseFailure)
	}
	if resumeCalled {
		t.Fatal("ResumeThread ran after the discovery snapshot failed to close")
	}
	if !threadClosed {
		t.Fatal("thread handle was not closed after the pre-resume snapshot failure")
	}
}

func TestResumeProcessSuccessfulResumeDoesNotRollbackForThreadCloseFailure(t *testing.T) {
	threadCloseFailure := errors.New("forced post-resume thread close failure")
	resumeCalled := false
	threadCloseCalled := false
	ops := fakeResumeProcessOperations(41, func(uint32) (windows.Handle, error) {
		return windows.Handle(2), nil
	}, func(windows.Handle) (uint32, error) {
		resumeCalled = true
		return 1, nil
	})
	ops.closeHandle = func(handle windows.Handle) error {
		if handle == windows.Handle(2) {
			threadCloseCalled = true
			return threadCloseFailure
		}
		return nil
	}

	if err := resumeProcessWithOperations(41, ops); err != nil {
		t.Fatalf("successful ResumeThread returned a rollback-triggering error: %v", err)
	}
	if !resumeCalled || !threadCloseCalled {
		t.Fatalf("resume/close operations = resume:%t close:%t, want both", resumeCalled, threadCloseCalled)
	}
}

func TestStartTrackedRequiredDoesNotRollbackAfterSuccessfulNativeResume(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-ran")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartTrackedRequiredHelper$")
	cmd.Env = append(os.Environ(),
		"GO_WANT_START_TRACKED_REQUIRED_HELPER=1",
		"START_TRACKED_REQUIRED_MARKER="+marker,
	)

	forcedThreadCloseFailure := errors.New("forced post-resume thread close failure")
	cleanupCalled := false
	closeCalls := 0
	job, err := startTrackedWithHooks(
		cmd,
		true,
		assignJob,
		func(pid uint32) error {
			ops := nativeResumeProcessOperations
			ops.closeHandle = func(handle windows.Handle) error {
				closeCalls++
				if err := windows.CloseHandle(handle); err != nil {
					return err
				}
				if closeCalls == 2 {
					return forcedThreadCloseFailure
				}
				return nil
			}
			return resumeProcessWithOperations(pid, ops)
		},
		func(cmd *exec.Cmd, job uintptr) error {
			cleanupCalled = true
			return terminateAndReapStartedProcess(cmd, job)
		},
	)
	if err != nil {
		t.Fatalf("StartTrackedRequired rolled back a successfully resumed child: %v", err)
	}
	if job == 0 {
		t.Fatal("StartTrackedRequired did not assign a Job Object")
	}
	waitStarted := false
	defer func() {
		if job != 0 {
			KillTracked(cmd, job)
			if !waitStarted && cmd.ProcessState == nil {
				_ = cmd.Wait()
			}
		}
	}()
	if cleanupCalled {
		t.Fatal("start rollback ran after ResumeThread succeeded")
	}
	if closeCalls != 2 {
		t.Fatalf("native snapshot/thread close calls = %d, want 2", closeCalls)
	}

	waitDone := make(chan error, 1)
	waitStarted = true
	go func() { waitDone <- cmd.Wait() }()
	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			t.Fatalf("wait for successfully resumed child: %v", waitErr)
		}
	case <-ctx.Done():
		t.Fatal("successfully resumed child did not exit")
	}
	FinishTracked(job)
	job = 0
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("successfully resumed child did not run: %v", statErr)
	}
}

func fakeResumeProcessOperations(
	pid uint32,
	openThread func(uint32) (windows.Handle, error),
	resumeThread func(windows.Handle) (uint32, error),
) resumeProcessOperations {
	return resumeProcessOperations{
		createSnapshot: func() (windows.Handle, error) { return windows.Handle(1), nil },
		threadFirst: func(_ windows.Handle, entry *windows.ThreadEntry32) error {
			entry.OwnerProcessID = pid
			entry.ThreadID = 99
			return nil
		},
		threadNext: func(windows.Handle, *windows.ThreadEntry32) error {
			return windows.ERROR_NO_MORE_FILES
		},
		openThread:   openThread,
		threadOwner:  func(windows.Handle) (uint32, error) { return pid, nil },
		resumeThread: resumeThread,
		closeHandle:  func(windows.Handle) error { return nil },
	}
}

func TestStartTrackedRequiredFailsClosedOnNativeResumeFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-ran")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStartTrackedRequiredHelper$")
	cmd.Env = append(os.Environ(),
		"GO_WANT_START_TRACKED_REQUIRED_HELPER=1",
		"START_TRACKED_REQUIRED_MARKER="+marker,
	)

	forcedResumeFailure := errors.New("forced native resume failure")
	type startResult struct {
		job         uintptr
		err         error
		assignedJob uintptr
		cleanedJob  uintptr
	}
	result := make(chan startResult, 1)
	go func() {
		var assignedJob uintptr
		var cleanedJob uintptr
		job, err := startTrackedWithHooks(
			cmd,
			true,
			func(cmd *exec.Cmd) uintptr {
				assignedJob = assignJob(cmd)
				return assignedJob
			},
			func(uint32) error { return forcedResumeFailure },
			func(cmd *exec.Cmd, job uintptr) error {
				cleanedJob = job
				return terminateAndReapStartedProcess(cmd, job)
			},
		)
		result <- startResult{job: job, err: err, assignedJob: assignedJob, cleanedJob: cleanedJob}
	}()

	var got startResult
	select {
	case got = <-result:
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		t.Fatal("StartTrackedRequired hung after a forced native resume failure")
	}
	if !errors.Is(got.err, ErrProcessTrackingUnavailable) {
		t.Fatalf("StartTrackedRequired error = %v, want ErrProcessTrackingUnavailable", got.err)
	}
	if !errors.Is(got.err, forcedResumeFailure) {
		t.Fatalf("StartTrackedRequired error = %v, want forced resume cause", got.err)
	}
	if got.job != 0 {
		t.Fatalf("StartTrackedRequired job = %d, want 0 after resume failure", got.job)
	}
	if got.assignedJob == 0 {
		t.Fatal("native test did not assign the suspended child to a Job Object")
	}
	if got.cleanedJob != got.assignedJob {
		t.Fatalf("cleanup job = %d, want assigned Job Object %d", got.cleanedJob, got.assignedJob)
	}
	if state := cmd.ProcessState; state == nil || !state.Exited() {
		t.Fatalf("resume-failed suspended child was not reaped: %#v", state)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("resume-failed child ran before fail-closed termination: %v", statErr)
	}
}

func TestStartTrackedRequiredHelper(t *testing.T) {
	if os.Getenv("GO_WANT_START_TRACKED_REQUIRED_HELPER") != "1" {
		return
	}
	marker := os.Getenv("START_TRACKED_REQUIRED_MARKER")
	if marker == "" {
		os.Exit(2)
	}
	if err := os.WriteFile(marker, []byte("ran"), 0o600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
