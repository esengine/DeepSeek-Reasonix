package sftp

// sftp server integration tests
// enable with -integration
// example invokation (darwin): gofmt -w `find . -name \*.go` && (cd server_standalone/ ; go build -tags debug) && go test -tags debug github.com/pkg/sftp -integration -v -sftp /usr/libexec/sftp-server -run ServerCompareSubsystems

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kr/fs"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"
)

func TestMain(m *testing.M) {
	sftpClientLocation, _ := exec.LookPath("sftp")
	testSftpClientBin = flag.String("sftp_client", sftpClientLocation, "location of the sftp client binary")

	lookSFTPServer := []string{
		"/usr/libexec/sftp-server",
		"/usr/lib/openssh/sftp-server",
		"/usr/lib/ssh/sftp-server",
		"C:\\Program Files\\Git\\usr\\lib\\ssh\\sftp-server.exe",
	}
	sftpServer, _ := exec.LookPath("sftp-server")
	if len(sftpServer) == 0 {
		for _, location := range lookSFTPServer {
			if _, err := os.Stat(location); err == nil {
				sftpServer = location
				break
			}
		}
	}
	testSftp = flag.String("sftp", sftpServer, "location of the sftp server binary")
	flag.Parse()

	os.Exit(m.Run())
}

func skipIfWindows(t testing.TB) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping test on windows")
	}
}

func skipIfPlan9(t testing.TB) {
	if runtime.GOOS == "plan9" {
		t.Skip("skipping test on plan9")
	}
}

var testServerImpl = flag.Bool("testserver", false, "perform integration tests against sftp package server instance")
var testIntegration = flag.Bool("integration", false, "perform integration tests against sftp server process")
var testAllocator = flag.Bool("allocator", false, "perform tests using the allocator")
var testSftp *string

var testSftpClientBin *string
var sshServerDebugStream = ioutil.Discard
var sftpServerDebugStream = ioutil.Discard
var sftpClientDebugStream = ioutil.Discard

const (
	GolangSFTP  = true
	OpenSSHSFTP = false
)

var (
	hostPrivateKeySigner ssh.Signer
	privKey              = []byte(`
-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEArhp7SqFnXVZAgWREL9Ogs+miy4IU/m0vmdkoK6M97G9NX/Pj
wf8I/3/ynxmcArbt8Rc4JgkjT2uxx/NqR0yN42N1PjO5Czu0dms1PSqcKIJdeUBV
7gdrKSm9Co4d2vwfQp5mg47eG4w63pz7Drk9+VIyi9YiYH4bve7WnGDswn4ycvYZ
slV5kKnjlfCdPig+g5P7yQYud0cDWVwyA0+kxvL6H3Ip+Fu8rLDZn4/P1WlFAIuc
PAf4uEKDGGmC2URowi5eesYR7f6GN/HnBs2776laNlAVXZUmYTUfOGagwLsEkx8x
XdNqntfbs2MOOoK+myJrNtcB9pCrM0H6um19uQIDAQABAoIBABkWr9WdVKvalgkP
TdQmhu3mKRNyd1wCl+1voZ5IM9Ayac/98UAvZDiNU4Uhx52MhtVLJ0gz4Oa8+i16
IkKMAZZW6ro/8dZwkBzQbieWUFJ2Fso2PyvB3etcnGU8/Yhk9IxBDzy+BbuqhYE2
1ebVQtz+v1HvVZzaD11bYYm/Xd7Y28QREVfFen30Q/v3dv7dOteDE/RgDS8Czz7w
jMW32Q8JL5grz7zPkMK39BLXsTcSYcaasT2ParROhGJZDmbgd3l33zKCVc1zcj9B
SA47QljGd09Tys958WWHgtj2o7bp9v1Ufs4LnyKgzrB80WX1ovaSQKvd5THTLchO
kLIhUAECgYEA2doGXy9wMBmTn/hjiVvggR1aKiBwUpnB87Hn5xCMgoECVhFZlT6l
WmZe7R2klbtG1aYlw+y+uzHhoVDAJW9AUSV8qoDUwbRXvBVlp+In5wIqJ+VjfivK
zgIfzomL5NvDz37cvPmzqIeySTowEfbQyq7CUQSoDtE9H97E2wWZhDkCgYEAzJdJ
k+NSFoTkHhfD3L0xCDHpRV3gvaOeew8524fVtVUq53X8m91ng4AX1r74dCUYwwiF
gqTtSSJfx2iH1xKnNq28M9uKg7wOrCKrRqNPnYUO3LehZEC7rwUr26z4iJDHjjoB
uBcS7nw0LJ+0Zeg1IF+aIdZGV3MrAKnrzWPixYECgYBsffX6ZWebrMEmQ89eUtFF
u9ZxcGI/4K8ErC7vlgBD5ffB4TYZ627xzFWuBLs4jmHCeNIJ9tct5rOVYN+wRO1k
/CRPzYUnSqb+1jEgILL6istvvv+DkE+ZtNkeRMXUndWwel94BWsBnUKe0UmrSJ3G
sq23J3iCmJW2T3z+DpXbkQKBgQCK+LUVDNPE0i42NsRnm+fDfkvLP7Kafpr3Umdl
tMY474o+QYn+wg0/aPJIf9463rwMNyyhirBX/k57IIktUdFdtfPicd2MEGETElWv
nN1GzYxD50Rs2f/jKisZhEwqT9YNyV9DkgDdGGdEbJNYqbv0qpwDIg8T9foe8E1p
bdErgQKBgAt290I3L316cdxIQTkJh1DlScN/unFffITwu127WMr28Jt3mq3cZpuM
Aecey/eEKCj+Rlas5NDYKsB18QIuAw+qqWyq0LAKLiAvP1965Rkc4PLScl3MgJtO
QYa37FK0p8NcDeUuF86zXBVutwS5nJLchHhKfd590ks57OROtm29
-----END RSA PRIVATE KEY-----
`)
)

func init() {
	var err error
	hostPrivateKeySigner, err = ssh.ParsePrivateKey(privKey)
	if err != nil {
		panic(err)
	}
}

func keyAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	permissions := &ssh.Permissions{
		CriticalOptions: map[string]string{},
		Extensions:      map[string]string{},
	}
	return permissions, nil
}

func pwAuth(conn ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
	permissions := &ssh.Permissions{
		CriticalOptions: map[string]string{},
		Extensions:      map[string]string{},
	}
	return permissions, nil
}

func basicServerConfig() *ssh.ServerConfig {
	config := ssh.ServerConfig{
		Config: ssh.Config{
			MACs: []string{"hmac-sha1"},
		},
		PasswordCallback:  pwAuth,
		PublicKeyCallback: keyAuth,
	}
	config.AddHostKey(hostPrivateKeySigner)
	return &config
}

type sshServer struct {
	useSubsystem bool
	conn         net.Conn
	config       *ssh.ServerConfig
	sshConn      *ssh.ServerConn
	newChans     <-chan ssh.NewChannel
	newReqs      <-chan *ssh.Request
}

func sshServerFromConn(conn net.Conn, useSubsystem bool, config *ssh.ServerConfig) (*sshServer, error) {
	// From a standard TCP connection to an encrypted SSH connection
	sshConn, newChans, newReqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return nil, err
	}

	svr := &sshServer{useSubsystem, conn, config, sshConn, newChans, newReqs}
	svr.listenChannels()
	return svr, nil
}

func (svr *sshServer) Wait() error {
	return svr.sshConn.Wait()
}

func (svr *sshServer) Close() error {
	return svr.sshConn.Close()
}

func (svr *sshServer) listenChannels() {
	go func() {
		for chanReq := range svr.newChans {
			go svr.handleChanReq(chanReq)
		}
	}()
	go func() {
		for req := range svr.newReqs {
			go svr.handleReq(req)
		}
	}()
}

func (svr *sshServer) handleReq(req *ssh.Request) {
	switch req.Type {
	default:
		rejectRequest(req)
	}
}

type sshChannelServer struct {
	svr     *sshServer
	chanReq ssh.NewChannel
	ch      ssh.Channel
	newReqs <-chan *ssh.Request
}

type sshSessionChannelServer struct {
	*sshChannelServer
	env []string
}

func (svr *sshServer) handleChanReq(chanReq ssh.NewChannel) {
	fmt.Fprintf(sshServerDebugStream, "channel request: %v, extra: '%v'\n", chanReq.ChannelType(), hex.EncodeToString(chanReq.ExtraData()))
	switch chanReq.ChannelType() {
	case "session":
		if ch, reqs, err := chanReq.Accept(); err != nil {
			fmt.Fprintf(sshServerDebugStream, "fail to accept channel request: %v\n", err)
			chanReq.Reject(ssh.ResourceShortage, "channel accept failure")
		} else {
			chsvr := &sshSessionChannelServer{
				sshChannelServer: &sshChannelServer{svr, chanReq, ch, reqs},
				env:              append([]string{}, os.Environ()...),
			}
			chsvr.handle()
		}
	default:
		chanReq.Reject(ssh.UnknownChannelType, "channel type is not a session")
	}
}

func (chsvr *sshSessionChannelServer) handle() {
	// should maybe do something here...
	go chsvr.handleReqs()
}

func (chsvr *sshSessionChannelServer) handleReqs() {
	for req := range chsvr.newReqs {
		chsvr.handleReq(req)
	}
	fmt.Fprintf(sshServerDebugStream, "ssh server session channel complete\n")
}

func (chsvr *sshSessionChannelServer) handleReq(req *ssh.Request) {
	switch req.Type {
	case "env":
		chsvr.handleEnv(req)
	case "subsystem":
		chsvr.handleSubsystem(req)
	default:
		rejectRequest(req)
	}
}

func rejectRequest(req *ssh.Request) error {
	fmt.Fprintf(sshServerDebugStream, "ssh rejecting request, type: %s\n", req.Type)
	err := req.Reply(false, []byte{})
	if err != nil {
		fmt.Fprintf(sshServerDebugStream, "ssh request reply had error: %v\n", err)
	}
	return err
}

func rejectRequestUnmarshalError(req *ssh.Request, s interface{}, err error) error {
	fmt.Fprintf(sshServerDebugStream, "ssh request unmarshaling error, type '%T': %v\n", s, err)
	rejectRequest(req)
	return err
}

// env request form:
type sshEnvRequest struct {
	Envvar string
	Value  string
}

func (chsvr *sshSessionChannelServer) handleEnv(req *ssh.Request) error {
	envReq := &sshEnvRequest{}
	if err := ssh.Unmarshal(req.Payload, envReq); err != nil {
		return rejectRequestUnmarshalError(req, envReq, err)
	}
	req.Reply(true, nil)

	found := false
	for i, envstr := range chsvr.env {
		if strings.HasPrefix(envstr, envReq.Envvar+"=") {
			found = true
			chsvr.env[i] = envReq.Envvar + "=" + envReq.Value
		}
	}
	if !found {
		chsvr.env = append(chsvr.env, envReq.Envvar+"="+envReq.Value)
	}

	return nil
}

// Payload: int: command size, string: command
type sshSubsystemRequest struct {
	Name string
}

type sshSubsystemExitStatus struct {
	Status uint32
}

func (chsvr *sshSessionChannelServer) handleSubsystem(req *ssh.Request) error {
	defer func() {
		err1 := chsvr.ch.CloseWrite()
		err2 := chsvr.ch.Close()
		fmt.Fprintf(sshServerDebugStream, "ssh server subsystem request complete, err: %v %v\n", err1, err2)
	}()

	subsystemReq := &sshSubsystemRequest{}
	if err := ssh.Unmarshal(req.Payload, subsystemReq); err != nil {
		return rejectRequestUnmarshalError(req, subsystemReq, err)
	}

	// reply to the ssh client

	// no idea if this is actually correct spec-wise.
	// just enough for an sftp server to start.
	if subsystemReq.Name != "sftp" {
		return req.Reply(false, nil)
	}

	req.Reply(true, nil)

	if !chsvr.svr.useSubsystem {
		// use the openssh sftp server backend; this is to test the ssh code, not the sftp code,
		// or is used for comparison between our sftp subsystem and the openssh sftp subsystem
		cmd := exec.Command(*testSftp, "-e", "-l", "DEBUG") // log to stderr
		cmd.Stdin = chsvr.ch
		cmd.Stdout = chsvr.ch
		cmd.Stderr = sftpServerDebugStream
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Wait()
	}

	sftpServer, err := NewServer(
		chsvr.ch,
		WithDebug(sftpServerDebugStream),
	)
	if err != nil {
		return err
	}

	// wait for the session to close
	runErr := sftpServer.Serve()
	exitStatus := uint32(1)
	if runErr == nil {
		exitStatus = uint32(0)
	}

	_, exitStatusErr := chsvr.ch.SendRequest("exit-status", false, ssh.Marshal(sshSubsystemExitStatus{exitStatus}))
	return exitStatusErr
}

// starts an ssh server to test. returns: host string and port
func testServer(t *testing.T, useSubsystem bool, readonly bool) (func(), string, int) {
	t.Helper()

	if !*testIntegration {
		t.Skip("skipping integration test")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	shutdown := make(chan struct{})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-shutdown:
				default:
					t.Error("ssh server socket closed:", err)
				}
				return
			}

			go func() {
				defer conn.Close()

				sshSvr, err := sshServerFromConn(conn, useSubsystem, basicServerConfig())
				if err != nil {
					t.Error(err)
					return
				}

				_ = sshSvr.Wait()
			}()
		}
	}()

	return func() { close(shutdown); listener.Close() }, host, port
}

func makeDummyKey() (string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		return "", fmt.Errorf("cannot generate key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", fmt.Errorf("cannot marshal key: %w", err)
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	f, err := ioutil.TempFile("", "sftp-test-key-")
	if err != nil {
		return "", fmt.Errorf("cannot create temp file: %w", err)
	}
	defer func() {
		if f != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	if err := pem.Encode(f, block); err != nil {
		return "", fmt.Errorf("cannot write key: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("error closing key file: %w", err)
	}
	path := f.Name()
	f = nil
	return path, nil
}

type execError struct {
	path   string
	stderr string
	err    error
}

func (e *execError) Error() string {
	return fmt.Sprintf("%s: %v: %s", e.path, e.err, e.stderr)
}

func (e *execError) Unwrap() error {
	return e.err
}

func (e *execError) Cause() error {
	return e.err
}

func runSftpClient(t *testing.T, script string, path string, host string, port int) (string, error) {
	// if sftp client binary is unavailable, skip test
	if _, err := os.Stat(*testSftpClientBin); err != nil {
		t.Skip("sftp client binary unavailable")
	}

	// make a dummy key so we don't rely on ssh-agent
	dummyKey, err := makeDummyKey()
	if err != nil {
		return "", err
	}
	defer os.Remove(dummyKey)

	cmd := exec.Command(
		*testSftpClientBin,
		// "-vvvv",
		"-b", "-",
		"-o", "StrictHostKeyChecking=no",
		"-o", "LogLevel=ERROR",
		"-o", "UserKnownHostsFile /dev/null",
		// do not trigger ssh-agent prompting
		"-o", "IdentityFile="+dummyKey,
		"-o", "IdentitiesOnly=yes",
		"-P", fmt.Sprintf("%d", port), fmt.Sprintf("%s:%s", host, path),
	)

	cmd.Stdin = strings.NewReader(script)

	stdout := new(bytes.Buffer)
	cmd.Stdout = stdout

	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return "", err
	}

	if err := cmd.Wait(); err != nil {
		return stdout.String(), &execError{
			path:   cmd.Path,
			stderr: stderr.String(),
			err:    err,
		}
	}

	return stdout.String(), nil
}

// assert.Eventually seems to have a data rate on macOS with go 1.14 so replace it with this simpler function
func waitForCondition(t *testing.T, condition func() bool) {
	start := time.Now()
	tick := 10 * time.Millisecond
	waitFor := 100 * time.Millisecond
	for !condition() {
		time.Sleep(tick)
		if time.Since(start) > waitFor {
			break
		}
	}
	assert.True(t, condition())
}

func checkAllocatorBeforeServerClose(t *testing.T, alloc *allocator) {
	if alloc != nil {
		// before closing the server we are, generally, waiting for new packets in recvPacket and we have a page allocated.
		// Sometime the sendPacket returns some milliseconds after the client receives the response, and so we have 2
		// allocated pages here, so wait some milliseconds. To avoid crashes we must be sure to not release the pages
		// too soon.
		waitForCondition(t, func() bool { return alloc.countUsedPages() <= 1 })
	}
}

func checkAllocatorAfterServerClose(t *testing.T, alloc *allocator) {
	if alloc != nil {
		// wait for the server cleanup
		waitForCondition(t, func() bool { return alloc.countUsedPages() == 0 })
		waitForCondition(t, func() bool { return alloc.countAvailablePages() == 0 })
	}
}

func TestServerCompareSubsystems(t *testing.T) {
	if runtime.GOOS == "windows" {
		// TODO (puellanivis): not sure how to fix this, the OpenSSH SFTP implementation closes immediately.
		t.Skip()
	}

	shutdownGo, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdownGo()

	shutdownOp, hostOp, portOp := testServer(t, OpenSSHSFTP, READONLY)
	defer shutdownOp()

	script := `
ls /
ls -l /
ls /dev/
ls -l /dev/
ls -l /etc/
ls -l /bin/
ls -l /usr/bin/
`
	outputGo, err := runSftpClient(t, script, "/", hostGo, portGo)
	if err != nil {
		t.Fatal(err)
	}

	outputOp, err := runSftpClient(t, script, "/", hostOp, portOp)
	if err != nil {
		t.Fatal(err)
	}

	newlineRegex := regexp.MustCompile(`\r*\n`)
	spaceRegex := regexp.MustCompile(`\s+`)
	outputGoLines := newlineRegex.Split(outputGo, -1)
	outputOpLines := newlineRegex.Split(outputOp, -1)

	if len(outputGoLines) != len(outputOpLines) {
		t.Fatalf("output line count differs, go = %d, openssh = %d", len(outputGoLines), len(outputOpLines))
	}

	for i, goLine := range outputGoLines {
		opLine := outputOpLines[i]
		bad := false
		if goLine != opLine {
			goWords := spaceRegex.Split(goLine, -1)
			opWords := spaceRegex.Split(opLine, -1)
			// some fields are allowed to be different..
			// during testing as processes are created/destroyed.
			for j, goWord := range goWords {
				if j >= len(opWords) {
					bad = true
					break
				}
				opWord := opWords[j]
				if goWord != opWord {
					switch j {
					case 1, 2, 3, 7:
						// words[1] as the link count for directories like proc is unstable
						// words[2] and [3] as these are users & groups
						// words[7] as timestamps on dirs can vary for things like /tmp
					case 8:
						// words[8] can either have full path or just the filename
						bad = !strings.HasSuffix(opWord, "/"+goWord)
					default:
						bad = true
					}
				}
			}
		}

		if bad {
			t.Errorf("outputs differ\n     go: %q\nopenssh: %q\n", goLine, opLine)
		}
	}
}

var rng = rand.New(rand.NewSource(time.Now().Unix()))

func randData(length int) []byte {
	data := make([]byte, length)
	for i := 0; i < length; i++ {
		data[i] = byte(rng.Uint32())
	}
	return data
}

func randName() string {
	return "sftp." + hex.EncodeToString(randData(16))
}

func TestServerMkdirRmdir(t *testing.T) {
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	tmpDir := "/tmp/" + randName()
	defer os.RemoveAll(tmpDir)

	// mkdir remote
	if _, err := runSftpClient(t, "mkdir "+tmpDir, "/", hostGo, portGo); err != nil {
		t.Fatal(err)
	}

	// directory should now exist
	if _, err := os.Stat(tmpDir); err != nil {
		t.Fatal(err)
	}

	// now remove the directory
	if _, err := runSftpClient(t, "rmdir "+tmpDir, "/", hostGo, portGo); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(tmpDir); err == nil {
		t.Fatal("should have error after deleting the directory")
	}
}

func TestServerLink(t *testing.T) {
	skipIfWindows(t) // No hard links on windows.
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	tmpFileLocalData := randData(999)

	linkdest := "/tmp/" + randName()
	defer os.RemoveAll(linkdest)
	if err := ioutil.WriteFile(linkdest, tmpFileLocalData, 0644); err != nil {
		t.Fatal(err)
	}

	link := "/tmp/" + randName()
	defer os.RemoveAll(link)

	// now create a hard link within the new directory
	if output, err := runSftpClient(t, fmt.Sprintf("ln %s %s", linkdest, link), "/", hostGo, portGo); err != nil {
		t.Fatalf("failed: %v %v", err, string(output))
	}

	// file should now exist and be the same size as linkdest
	if stat, err := os.Lstat(link); err != nil {
		t.Fatal(err)
	} else if int(stat.Size()) != len(tmpFileLocalData) {
		t.Fatalf("wrong size: %v", len(tmpFileLocalData))
	}
}

func TestServerSymlink(t *testing.T) {
	skipIfWindows(t) // No symlinks on windows.
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	link := "/tmp/" + randName()
	defer os.RemoveAll(link)

	// now create a symbolic link within the new directory
	if output, err := runSftpClient(t, "symlink /bin/sh "+link, "/", hostGo, portGo); err != nil {
		t.Fatalf("failed: %v %v", err, string(output))
	}

	// symlink should now exist
	if stat, err := os.Lstat(link); err != nil {
		t.Fatal(err)
	} else if (stat.Mode() & os.ModeSymlink) != os.ModeSymlink {
		t.Fatalf("is not a symlink: %v", stat.Mode())
	}
}

func TestServerPut(t *testing.T) {
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	tmpFileLocal := "/tmp/" + randName()
	tmpFileRemote := "/tmp/" + randName()
	defer os.RemoveAll(tmpFileLocal)
	defer os.RemoveAll(tmpFileRemote)

	t.Logf("put: local %v remote %v", tmpFileLocal, tmpFileRemote)

	// create a file with random contents. This will be the local file pushed to the server
	tmpFileLocalData := randData(10 * 1024 * 1024)
	if err := ioutil.WriteFile(tmpFileLocal, tmpFileLocalData, 0644); err != nil {
		t.Fatal(err)
	}

	// sftp the file to the server
	if output, err := runSftpClient(t, "put "+tmpFileLocal+" "+tmpFileRemote, "/", hostGo, portGo); err != nil {
		t.Fatalf("runSftpClient failed: %v, output\n%v\n", err, output)
	}

	// tmpFile2 should now exist, with the same contents
	if tmpFileRemoteData, err := ioutil.ReadFile(tmpFileRemote); err != nil {
		t.Fatal(err)
	} else if string(tmpFileLocalData) != string(tmpFileRemoteData) {
		t.Fatal("contents of file incorrect after put")
	}
}

func TestServerResume(t *testing.T) {
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	tmpFileLocal := "/tmp/" + randName()
	tmpFileRemote := "/tmp/" + randName()
	defer os.RemoveAll(tmpFileLocal)
	defer os.RemoveAll(tmpFileRemote)

	t.Logf("put: local %v remote %v", tmpFileLocal, tmpFileRemote)

	// create a local file with random contents to be pushed to the server
	tmpFileLocalData := randData(2 * 1024 * 1024)
	// only write half the data to simulate a split upload
	half := 1024 * 1024
	err := ioutil.WriteFile(tmpFileLocal, tmpFileLocalData[:half], 0644)
	if err != nil {
		t.Fatal(err)
	}

	// sftp the first half of the file to the server
	output, err := runSftpClient(t, "put "+tmpFileLocal+" "+tmpFileRemote,
		"/", hostGo, portGo)
	if err != nil {
		t.Fatalf("runSftpClient failed: %v, output\n%v\n", err, output)
	}

	// write the full file out
	err = ioutil.WriteFile(tmpFileLocal, tmpFileLocalData, 0644)
	if err != nil {
		t.Fatal(err)
	}
	// re-sftp the full file with the append flag set
	output, err = runSftpClient(t, "put -a "+tmpFileLocal+" "+tmpFileRemote,
		"/", hostGo, portGo)
	if err != nil {
		t.Fatalf("runSftpClient failed: %v, output\n%v\n", err, output)
	}

	// tmpFileRemote should now exist, with the same contents
	if tmpFileRemoteData, err := ioutil.ReadFile(tmpFileRemote); err != nil {
		t.Fatal(err)
	} else if string(tmpFileLocalData) != string(tmpFileRemoteData) {
		t.Fatal("contents of file incorrect after put")
	}
}

func TestServerGet(t *testing.T) {
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	tmpFileLocal := "/tmp/" + randName()
	tmpFileRemote := "/tmp/" + randName()
	defer os.RemoveAll(tmpFileLocal)
	defer os.RemoveAll(tmpFileRemote)

	t.Logf("get: local %v remote %v", tmpFileLocal, tmpFileRemote)

	// create a file with random contents. This will be the remote file pulled from the server
	tmpFileRemoteData := randData(10 * 1024 * 1024)
	if err := ioutil.WriteFile(tmpFileRemote, tmpFileRemoteData, 0644); err != nil {
		t.Fatal(err)
	}

	// sftp the file to the server
	if output, err := runSftpClient(t, "get "+tmpFileRemote+" "+tmpFileLocal, "/", hostGo, portGo); err != nil {
		t.Fatalf("runSftpClient failed: %v, output\n%v\n", err, output)
	}

	// tmpFile2 should now exist, with the same contents
	if tmpFileLocalData, err := ioutil.ReadFile(tmpFileLocal); err != nil {
		t.Fatal(err)
	} else if string(tmpFileLocalData) != string(tmpFileRemoteData) {
		t.Fatal("contents of file incorrect after put")
	}
}

func compareDirectoriesRecursive(t *testing.T, aroot, broot string) {
	walker := fs.Walk(aroot)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			t.Fatal(err)
		}
		// find paths
		aPath := walker.Path()
		aRel, err := filepath.Rel(aroot, aPath)
		if err != nil {
			t.Fatalf("could not find relative path for %v: %v", aPath, err)
		}
		bPath := filepath.Join(broot, aRel)

		if aRel == "." {
			continue
		}

		//t.Logf("comparing: %v a: %v b %v", aRel, aPath, bPath)

		// if a is a link, the sftp recursive copy won't have copied it. ignore
		aLink, err := os.Lstat(aPath)
		if err != nil {
			t.Fatalf("could not lstat %v: %v", aPath, err)
		}
		if aLink.Mode()&os.ModeSymlink != 0 {
			continue
		}

		// stat the files
		aFile, err := os.Stat(aPath)
		if err != nil {
			t.Fatalf("could not stat %v: %v", aPath, err)
		}
		bFile, err := os.Stat(bPath)
		if err != nil {
			t.Fatalf("could not stat %v: %v", bPath, err)
		}

		// compare stats, with some leniency for the timestamp
		if aFile.Mode() != bFile.Mode() {
			t.Fatalf("modes different for %v: %v vs %v", aRel, aFile.Mode(), bFile.Mode())
		}
		if !aFile.IsDir() {
			if aFile.Size() != bFile.Size() {
				t.Fatalf("sizes different for %v: %v vs %v", aRel, aFile.Size(), bFile.Size())
			}
		}
		timeDiff := aFile.ModTime().Sub(bFile.ModTime())
		if timeDiff > time.Second || timeDiff < -time.Second {
			t.Fatalf("mtimes different for %v: %v vs %v", aRel, aFile.ModTime(), bFile.ModTime())
		}

		// compare contents
		if !aFile.IsDir() {
			if aContents, err := ioutil.ReadFile(aPath); err != nil {
				t.Fatal(err)
			} else if bContents, err := ioutil.ReadFile(bPath); err != nil {
				t.Fatal(err)
			} else if string(aContents) != string(bContents) {
				t.Fatalf("contents different for %v", aRel)
			}
		}
	}
}

func TestServerPutRecursive(t *testing.T) {
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	dirLocal, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDirRemote := "/tmp/" + randName()
	defer os.RemoveAll(tmpDirRemote)

	t.Logf("put recursive: local %v remote %v", dirLocal, tmpDirRemote)

	// On windows, the client copies the contents of the directory, not the directory itself.
	winFix := ""
	if runtime.GOOS == "windows" {
		winFix = "/" + filepath.Base(dirLocal)
	} //*/

	// push this directory (source code etc) recursively to the server
	if output, err := runSftpClient(t, "mkdir "+tmpDirRemote+"\r\nput -R -p "+dirLocal+" "+tmpDirRemote+winFix, "/", hostGo, portGo); err != nil {
		t.Fatalf("runSftpClient failed: %v, output\n%v\n", err, output)
	}

	compareDirectoriesRecursive(t, dirLocal, filepath.Join(tmpDirRemote, filepath.Base(dirLocal)))
}

func TestServerGetRecursive(t *testing.T) {
	shutdown, hostGo, portGo := testServer(t, GolangSFTP, READONLY)
	defer shutdown()

	dirRemote, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmpDirLocal := "/tmp/" + randName()
	defer os.RemoveAll(tmpDirLocal)

	t.Logf("get recursive: local %v remote %v", tmpDirLocal, dirRemote)

	// On windows, the client copies the contents of the directory, not the directory itself.
	winFix := ""
	if runtime.GOOS == "windows" {
		winFix = "/" + filepath.Base(dirRemote)
	}

	// pull this directory (source code etc) recursively from the server
	if output, err := runSftpClient(t, "lmkdir "+tmpDirLocal+"\r\nget -R -p "+dirRemote+" "+tmpDirLocal+winFix, "/", hostGo, portGo); err != nil {
		t.Fatalf("runSftpClient failed: %v, output\n%v\n", err, output)
	}

	compareDirectoriesRecursive(t, dirRemote, filepath.Join(tmpDirLocal, filepath.Base(dirRemote)))
}
