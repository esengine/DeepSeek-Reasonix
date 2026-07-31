package dbus

import (
	"bufio"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// tests whether AUTH EXTERNAL is successful connecting to
// a server in a different user-namespace
// if AUTH EXTERNAL sends the UID of the client
// it will clash with out-of-band credentials derived by server
// from underlying UDS and authentication will be rejected
func TestConnectToDifferentUserNamespace(t *testing.T) {
	addr, process := startDaemonInDifferentUserNamespace(t)
	defer func() { _ = process.Kill() }()
	conn, err := Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	if conn.Connected() {
		t.Fatal("Should be closed")
	}
}

// starts a dbus-daemon instance in a new user-namespace
// and returns its address string and underlying process.
func startDaemonInDifferentUserNamespace(t *testing.T) (string, *os.Process) {
	config := `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN"
	"http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
   <busconfig>
   <listen>unix:path=/tmp/test.socket</listen>
   <auth>EXTERNAL</auth>
   <apparmor mode="disabled"/>
   
	<policy context='default'>
	  <allow send_destination='*' eavesdrop='true'/>
	  <allow eavesdrop='true'/>
	  <allow user='*'/>
	</policy>   
   </busconfig>
   `
	cfg, err := os.CreateTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cfg.Name())
	if _, err = cfg.Write([]byte(config)); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("dbus-daemon", "--nofork", "--print-address", "--config-file", cfg.Name())

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID | syscall.CLONE_NEWUSER,
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      os.Getgid(),
				Size:        1,
			},
		},
	}

	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(out)
	l, _, err := r.ReadLine()
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	return string(l), cmd.Process
}
