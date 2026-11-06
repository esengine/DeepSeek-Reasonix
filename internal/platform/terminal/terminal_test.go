package terminal

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// oneShot is a shell that runs a single command and ends, with its exit code.
func oneShot(script string) Options {
	if runtime.GOOS == "windows" {
		return Options{Shell: "cmd.exe", Args: []string{"/c", script}, Name: "cmd"}
	}
	return Options{Shell: "/bin/sh", Args: []string{"-c", script}, Name: "sh"}
}

func drain(t *testing.T, s *Session) string {
	t.Helper()
	sub := s.Subscribe()
	defer sub.Cancel()
	var out bytes.Buffer
	out.Write(sub.Replay)
	timeout := time.After(15 * time.Second)
	for {
		select {
		case c, ok := <-sub.Output:
			if !ok {
				return out.String()
			}
			out.Write(c)
		case <-timeout:
			t.Fatalf("the shell did not end; output so far: %q", out.String())
		}
	}
}

func TestRingKeepsTheNewestBytesAndCountsAll(t *testing.T) {
	r := newRing(8)
	r.write([]byte("abcd"))
	r.write([]byte("efghij"))
	got, at := r.since(0)
	if string(got) != "cdefghij" || at != 10 {
		t.Fatalf("since(0) = %q at %d, want %q at 10", got, at, "cdefghij")
	}
	got, _ = r.since(7)
	if string(got) != "hij" {
		t.Fatalf("since(7) = %q, want hij", got)
	}
	r.write(bytes.Repeat([]byte("z"), 20))
	if got, _ = r.since(0); len(got) != 8 {
		t.Fatalf("a write larger than the ring keeps %d bytes, want 8", len(got))
	}
}

func TestAShellsOutputReachesASubscriberAndItsExitCodeIsKept(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, err := m.Open(oneShot("echo probe-xyz"))
	if err != nil {
		t.Fatal(err)
	}
	if out := drain(t, s); !strings.Contains(out, "probe-xyz") {
		t.Fatalf("output %q lacks the echoed text", out)
	}
	<-s.Done()
	info := s.Info()
	if info.Running || info.ExitCode == nil || *info.ExitCode != 0 {
		t.Fatalf("info after a clean exit = %+v", info)
	}
}

func TestANonzeroExitCodeIsReportedAsItWas(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, err := m.Open(oneShot("exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	drain(t, s)
	<-s.Done()
	if c := s.Info().ExitCode; c == nil || *c != 3 {
		t.Fatalf("exit code = %v, want 3", c)
	}
}

func TestALateSubscriberReplaysWhatWasMissed(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, err := m.Open(oneShot("echo late-replay"))
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	sub := s.Subscribe()
	defer sub.Cancel()
	if !strings.Contains(string(sub.Replay), "late-replay") {
		t.Fatalf("replay %q lacks the output written before subscribing", sub.Replay)
	}
	if _, open := <-sub.Output; open {
		t.Fatal("a subscription to an ended terminal must be closed")
	}
}

func TestWritingToAnEndedShellSaysSoByCode(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, err := m.Open(oneShot("exit 0"))
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	if err := s.Write([]byte("x")); CodeOf(err) != CodeExited {
		t.Fatalf("write error code = %q, want %q", CodeOf(err), CodeExited)
	}
}

func TestAnInteractiveShellTakesKeystrokes(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	o := Options{Shell: "/bin/sh", Name: "sh"}
	input := "echo typed-line\nexit\n"
	if runtime.GOOS == "windows" {
		o = Options{Shell: "cmd.exe", Name: "cmd"}
		input = "echo typed-line\r\nexit\r\n"
	}
	s, err := m.Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if out := drain(t, s); !strings.Contains(out, "typed-line") {
		t.Fatalf("output %q lacks what was typed", out)
	}
}

func TestRestartStartsTheShellAgainOnlyOnceItHasEnded(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	s, err := m.Open(oneShot("echo first"))
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	if err := s.Restart(); err != nil {
		t.Fatal(err)
	}
	if out := drain(t, s); !strings.Contains(out, "first") {
		t.Fatalf("restarted output %q", out)
	}
	<-s.Done()
	if s.Info().Running {
		t.Fatal("the restarted shell should have ended too")
	}
}

func TestOpenRefusesWithCodes(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	if _, err := m.Open(Options{}); CodeOf(err) != CodeShellMissing {
		t.Fatalf("no shell: code %q", CodeOf(err))
	}
	if _, err := m.Open(Options{Shell: "x", Dir: "Z:/no/such/folder"}); CodeOf(err) != CodeStartFailed {
		t.Fatalf("missing folder: code %q", CodeOf(err))
	}
	if _, err := m.Get("nope"); CodeOf(err) != CodeNotFound {
		t.Fatalf("unknown id: code %q", CodeOf(err))
	}
	if err := m.Close("nope"); CodeOf(err) != CodeNotFound {
		t.Fatalf("close unknown: code %q", CodeOf(err))
	}
	var e *Error
	if !errors.As(&Error{Code: CodeLimit}, &e) {
		t.Fatal("Error must be matchable with errors.As")
	}
}

func TestShellsListsOnlyWhatIsThere(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "pwsh" || name == "cmd" {
			return `C:\bin\` + name + ".exe", nil
		}
		return "", errors.New("missing")
	}
	exists := func(p string) bool {
		return strings.HasSuffix(p, `Git\bin\bash.exe`) && strings.HasPrefix(p, `C:\Program Files`)
	}
	env := func(k string) string {
		if k == "ProgramFiles" {
			return `C:\Program Files`
		}
		return ""
	}
	got := shellsOn("windows", env, look, exists)
	var names []string
	for _, s := range got {
		names = append(names, s.Kind)
	}
	if strings.Join(names, ",") != "bash,pwsh,cmd" {
		t.Fatalf("shells = %v, want bash,pwsh,cmd (best first, nothing absent)", names)
	}
}

func TestShellsOnUnixStartsWithThePersonsOwn(t *testing.T) {
	look := func(name string) (string, error) { return "/usr/bin/" + name, nil }
	got := shellsOn("linux", func(k string) string {
		if k == "SHELL" {
			return "/usr/bin/zsh"
		}
		return ""
	}, look, func(string) bool { return true })
	if len(got) == 0 || got[0].Kind != "zsh" {
		t.Fatalf("shells = %+v, want the SHELL one first", got)
	}
	for i := range got {
		for j := i + 1; j < len(got); j++ {
			if got[i].Path == got[j].Path {
				t.Fatalf("%s listed twice", got[i].Path)
			}
		}
	}
}

func TestPersonEnvLeavesTheHostsOwnSecretsBehind(t *testing.T) {
	got := PersonEnv([]string{"PATH=/bin", "DEEPSEEK_API_KEY=k", "REASONIX_HOME=/h", "reasonix_token=t", "HOME=/u"})
	if strings.Join(got, ",") != "PATH=/bin,HOME=/u" {
		t.Fatalf("env = %v", got)
	}
}

func TestBusyIsTrueWhileTheShellWaitsOnACommandItStarted(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	o, wait := Options{Shell: "/bin/sh", Name: "sh"}, "sleep 3\n"
	if runtime.GOOS == "windows" {
		o, wait = Options{Shell: "cmd.exe", Name: "cmd"}, "ping -n 4 127.0.0.1 >nul\r\n"
	}
	s, err := m.Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if s.Info().Busy {
		t.Fatal("an idle shell reported busy")
	}
	if err := s.Write([]byte(wait)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !s.Info().Busy {
		if time.Now().After(deadline) {
			t.Fatal("a shell running a command never reported busy")
		}
		time.Sleep(50 * time.Millisecond)
	}
	for s.Info().Busy {
		if time.Now().After(deadline.Add(10 * time.Second)) {
			t.Fatal("busy never cleared after the command ended")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
