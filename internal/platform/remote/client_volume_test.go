package remote

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote/execfs/fstest"
	"reasonix/internal/platform/remote/sftpfs"
	"reasonix/internal/platform/remote/sshtest"
)

func startedClient(t *testing.T, srv *sshtest.Server) *Client {
	t.Helper()
	c := newTestClient(t, srv, Options{Auth: AuthOptions{
		DisableAgent: true,
		Password:     func() (string, error) { return "pw", nil },
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestAServerThatServesSFTPKeepsTheSFTPLayer(t *testing.T) {
	c := startedClient(t, sshtest.Start(t, sshtest.Options{Password: "pw", SFTPRoot: testenv.TempDir(t)}))
	v, err := c.Volume()
	if err != nil || v.Mode() != sftpfs.ModeSFTP {
		t.Fatalf("Volume = %v, %v; want sftp", v, err)
	}
}

func TestAServerThatRefusesTheSubsystemGetsTheScriptLayerAndKeepsIt(t *testing.T) {
	c := startedClient(t, sshtest.Start(t, sshtest.Options{Password: "pw"}))
	v, err := c.Volume()
	if err != nil {
		t.Fatalf("Volume: %v", err)
	}
	if v.Mode() != sftpfs.ModeExec {
		t.Fatalf("mode = %q, want exec", v.Mode())
	}
	again, err := c.Volume()
	if err != nil || again != v {
		t.Fatalf("a second call = %v, %v; want the layer the first one chose, not another probe", again, err)
	}
	files, err := c.Files()
	if err != nil || sftpfs.ModeOf(files) != sftpfs.ModeExec {
		t.Fatalf("Files = %v, %v; bootstrap must get the same layer", files, err)
	}
}

func TestAReconnectAsksTheServerAgain(t *testing.T) {
	c := startedClient(t, sshtest.Start(t, sshtest.Options{Password: "pw"}))
	if _, err := c.Volume(); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	cl := c.ssh
	c.mu.Unlock()
	c.installConn(cl, nil)
	c.mu.Lock()
	remembered := c.volume
	c.mu.Unlock()
	if remembered != nil {
		t.Fatal("a new connection kept the old connection's answer")
	}
}

func TestADeadLinkIsNotMistakenForARefusal(t *testing.T) {
	c := startedClient(t, sshtest.Start(t, sshtest.Options{Password: "pw"}))
	cl, err := c.SSH()
	if err != nil {
		t.Fatal(err)
	}
	_ = cl.Close()
	if v, err := c.Volume(); err == nil {
		t.Fatalf("Volume on a closed link = %v, want its error", v)
	}
	c.mu.Lock()
	remembered := c.volume
	c.mu.Unlock()
	if remembered != nil {
		t.Fatal("a failure that was not a refusal was remembered as one")
	}
}

func TestTheScriptLayerThroughARealShellOverSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test server runs exec requests under /bin/sh")
	}
	if err := exec.Command("stat", "-c", "%s", "/").Run(); err != nil {
		t.Skip("needs a GNU userland")
	}
	c := startedClient(t, sshtest.Start(t, sshtest.Options{Password: "pw", Shell: true}))
	v, err := c.Volume()
	if err != nil || v.Mode() != sftpfs.ModeExec {
		t.Fatalf("Volume = %v, %v", v, err)
	}
	fstest.RunScenario(t, v, testenv.TempDir(t))
}
