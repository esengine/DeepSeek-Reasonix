// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/internal/testenv"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
)

// serverPort contains the port that OpenSSH will listen on. OpenSSH can't take
// "0" as an argument here so we have to pick a number and hope that it's not in
// use on the machine. Since this only occurs when -update is given and thus
// when there's a human watching the test, this isn't too bad.
const serverPort = 24222

var (
	storeUsernameOnce sync.Once
)

type clientTest struct {
	// name is a freeform string identifying the test and the file in which
	// the expected results will be stored.
	name string
	// config contains the client configuration to use for this test.
	config *ssh.ClientConfig
	// expectError defines the error string to check if the connection is
	// expected to fail.
	expectError string
	// successCallback defines a callback to execute after the client connection
	// is established.
	successCallback func(t *testing.T, client *ssh.Client)
}

// connFromCommand starts the reference server process, connects to it and
// returns a recordingConn for the connection. It must be closed before Waiting
// for child.
func (test *clientTest) connFromCommand(t *testing.T, config string) *recordingConn {
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		t.Skipf("sshd not found, skipping test: %v", err)
	}
	dir, err := os.MkdirTemp("", "sshtest")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "sshd_config"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := configTmpl[config]; ok == false {
		t.Fatal(fmt.Errorf("Invalid server config '%s'", config))
	}
	configVars := map[string]string{
		"Dir": dir,
	}
	err = configTmpl[config].Execute(f, configVars)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	writeFile(filepath.Join(dir, "banner"), []byte("Server Banner"))

	for k, v := range testdata.PEMBytes {
		filename := "id_" + k
		writeFile(filepath.Join(dir, filename), v)
		writeFile(filepath.Join(dir, filename+".pub"), ssh.MarshalAuthorizedKey(testPublicKeys[k]))
	}

	var authkeys bytes.Buffer
	for k := range testdata.PEMBytes {
		authkeys.Write(ssh.MarshalAuthorizedKey(testPublicKeys[k]))
	}
	writeFile(filepath.Join(dir, "authorized_keys"), authkeys.Bytes())
	cmd := testenv.Command(t, sshd, "-D", "-e", "-f", f.Name(), "-p", strconv.Itoa(serverPort))
	cmd.Stdin = nil
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
		// Don't check for errors; if it fails it's most
		// likely "os: process already finished", and we don't
		// care about that. Use os.Interrupt, so child
		// processes are killed too.
		cmd.Process.Signal(os.Interrupt)
		cmd.Wait()
		if t.Failed() {
			t.Logf("OpenSSH output:\n\n%s", cmd.Stdout)
		}
	})
	var tcpConn net.Conn
	for i := uint(0); i < 5; i++ {
		tcpConn, err = net.DialTCP("tcp", nil, &net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: serverPort,
		})
		if err == nil {
			break
		}
		time.Sleep((1 << i) * 5 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("error connecting to the OpenSSH server: %v (%v)\n\n%s", err, cmd.Wait(), output.Bytes())
	}

	record := &recordingConn{
		Conn:           tcpConn,
		clientToServer: true,
	}

	return record
}

func (test *clientTest) dataPath() string {
	return filepath.Join("..", "testdata", "Client-"+test.name)
}

func (test *clientTest) usernameDataPath() string {
	return filepath.Join("..", "testdata", "Client-username")
}

func (test *clientTest) loadData() (flows [][]byte, err error) {
	in, err := os.Open(test.dataPath())
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return parseTestData(in)
}

func (test *clientTest) storeUsername() (err error) {
	storeUsernameOnce.Do(func() {
		err = os.WriteFile(test.usernameDataPath(), []byte(username()), 0666)
	})
	return err
}

func (test *clientTest) loadUsername() (string, error) {
	data, err := os.ReadFile(test.usernameDataPath())
	return string(data), err
}

func (test *clientTest) run(t *testing.T, write bool) {
	var clientConn net.Conn
	var recordingConn *recordingConn

	setDeterministicRandomSource(&test.config.Config)

	if write {
		// We store the username used when we record the connection so we can
		// reuse the same username when running tests.
		if err := test.storeUsername(); err != nil {
			t.Fatalf("failed to store username to %q: %v", test.usernameDataPath(), err)
		}
		recordingConn = test.connFromCommand(t, "default")
		clientConn = recordingConn
	} else {
		username, err := test.loadUsername()
		if err != nil {
			t.Fatalf("failed to load username from %q: %v", test.usernameDataPath(), err)
		}
		test.config.User = username
		timer := time.AfterFunc(10*time.Second, func() {
			fmt.Println("This test may be stuck, try running using -timeout 10s")
		})
		t.Cleanup(func() {
			timer.Stop()
		})
		flows, err := test.loadData()
		if err != nil {
			t.Fatalf("failed to load data from %s: %v", test.dataPath(), err)
		}
		clientConn = newReplayingConn(t, flows)
	}
	c, chans, reqs, err := ssh.NewClientConn(clientConn, "", test.config)
	if err != nil {
		if test.expectError == "" {
			t.Fatal(err)
		} else {
			if !strings.Contains(err.Error(), test.expectError) {
				t.Fatalf("%q not found in %v", test.expectError, err)
			}
		}
	} else {
		if test.expectError != "" {
			t.Error("dial should have failed.")
		}
		client := ssh.NewClient(c, chans, reqs)
		if test.successCallback != nil {
			test.successCallback(t, client)
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if write {
		path := test.dataPath()
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			t.Fatalf("Failed to create output file: %v", err)
		}
		defer out.Close()
		recordingConn.Close()

		recordingConn.WriteTo(out)
		t.Logf("Wrote %s\n", path)
	}
}

func recordingsClientConfig() *ssh.ClientConfig {
	config := clientConfig()
	config.SetDefaults()
	// Remove ML-KEM since it only works with Go 1.24.
	if config.KeyExchanges[0] == ssh.KeyExchangeMLKEM768X25519 {
		config.KeyExchanges = config.KeyExchanges[1:]
	}
	config.Auth = []ssh.AuthMethod{
		ssh.PublicKeys(testSigners["rsa"]),
	}
	return config
}

func TestClientKeyExchanges(t *testing.T) {
	config := ssh.ClientConfig{}
	config.SetDefaults()

	var keyExchanges []string
	for _, kex := range config.KeyExchanges {
		// Exclude ecdh for now, to make them deterministic we should use see a
		// stream of fixed bytes as the random source.
		if !strings.HasPrefix(kex, "ecdh-") {
			keyExchanges = append(keyExchanges, kex)
		}
	}
	// Add diffie-hellman-group-exchange-sha256 and
	// diffie-hellman-group16-sha512 as they are not enabled by default.
	keyExchanges = append(keyExchanges, "diffie-hellman-group-exchange-sha256", "diffie-hellman-group16-sha512")

	for _, kex := range keyExchanges {
		c := recordingsClientConfig()
		c.KeyExchanges = []string{kex}
		test := clientTest{
			name:   "KEX-" + kex,
			config: c,
		}
		runTestAndUpdateIfNeeded(t, test.name, test.run)
	}
}

func TestClientCiphers(t *testing.T) {
	config := ssh.ClientConfig{}
	config.SetDefaults()

	for _, ciph := range config.Ciphers {
		c := recordingsClientConfig()
		c.Ciphers = []string{ciph}
		test := clientTest{
			name:   "Cipher-" + ciph,
			config: c,
		}
		runTestAndUpdateIfNeeded(t, test.name, test.run)
	}
}

func TestClientMACs(t *testing.T) {
	config := ssh.ClientConfig{}
	config.SetDefaults()

	for _, mac := range config.MACs {
		c := recordingsClientConfig()
		c.MACs = []string{mac}
		test := clientTest{
			name:   "MAC-" + mac,
			config: c,
		}
		runTestAndUpdateIfNeeded(t, test.name, test.run)
	}
}

func TestBannerCallback(t *testing.T) {
	var receivedBanner string
	config := recordingsClientConfig()
	config.BannerCallback = func(message string) error {
		receivedBanner = message
		return nil
	}
	test := clientTest{
		name:   "BannerCallback",
		config: config,
		successCallback: func(t *testing.T, client *ssh.Client) {
			expected := "Server Banner"
			if receivedBanner != expected {
				t.Fatalf("got %v; want %v", receivedBanner, expected)
			}
		},
	}
	runTestAndUpdateIfNeeded(t, test.name, test.run)
}

func TestRunCommandSuccess(t *testing.T) {
	if runtime.GOARCH == "wasm" {
		t.Skip("skipping test, executing a command, session.Run(), is not supported on wasm")
	}
	test := clientTest{
		name:   "RunCommandSuccess",
		config: recordingsClientConfig(),
		successCallback: func(t *testing.T, client *ssh.Client) {
			session, err := client.NewSession()
			if err != nil {
				t.Fatalf("session failed: %v", err)
			}
			defer session.Close()
			err = session.Run("true")
			if err != nil {
				t.Fatalf("session failed: %v", err)
			}
		},
	}

	runTestAndUpdateIfNeeded(t, test.name, test.run)
}

func TestHostKeyCheck(t *testing.T) {
	config := recordingsClientConfig()
	hostDB := hostKeyDB()
	config.HostKeyCallback = hostDB.Check

	// change the keys.
	hostDB.keys[ssh.KeyAlgoRSA][25]++
	hostDB.keys[ssh.InsecureKeyAlgoDSA][25]++
	hostDB.keys[ssh.KeyAlgoECDSA256][25]++

	test := clientTest{
		name:        "HostKeyCheck",
		config:      config,
		expectError: "host key mismatch",
	}

	runTestAndUpdateIfNeeded(t, test.name, test.run)
}

func TestRunCommandStdin(t *testing.T) {
	if runtime.GOARCH == "wasm" {
		t.Skip("skipping test, executing a command, session.Run(), is not supported on wasm")
	}
	test := clientTest{
		name:   "RunCommandStdin",
		config: recordingsClientConfig(),
		successCallback: func(t *testing.T, client *ssh.Client) {
			session, err := client.NewSession()
			if err != nil {
				t.Fatalf("session failed: %v", err)
			}
			defer session.Close()

			r, w := io.Pipe()
			defer r.Close()
			defer w.Close()
			session.Stdin = r

			err = session.Run("true")
			if err != nil {
				t.Fatalf("session failed: %v", err)
			}
		},
	}

	runTestAndUpdateIfNeeded(t, test.name, test.run)
}

func TestRunCommandStdinError(t *testing.T) {
	if runtime.GOARCH == "wasm" {
		t.Skip("skipping test, executing a command, session.Run(), is not supported on wasm")
	}
	test := clientTest{
		name:   "RunCommandStdinError",
		config: recordingsClientConfig(),
		successCallback: func(t *testing.T, client *ssh.Client) {
			session, err := client.NewSession()
			if err != nil {
				t.Fatalf("session failed: %v", err)
			}
			defer session.Close()

			r, w := io.Pipe()
			defer r.Close()
			session.Stdin = r
			pipeErr := errors.New("closing write end of pipe")
			w.CloseWithError(pipeErr)

			err = session.Run("true")
			if err != pipeErr {
				t.Fatalf("expected %v, found %v", pipeErr, err)
			}
		},
	}

	runTestAndUpdateIfNeeded(t, test.name, test.run)
}

func TestRunCommandFailed(t *testing.T) {
	if runtime.GOARCH == "wasm" {
		t.Skip("skipping test, executing a command, session.Run(), is not supported on wasm")
	}
	test := clientTest{
		name:   "RunCommandFailed",
		config: recordingsClientConfig(),
		successCallback: func(t *testing.T, client *ssh.Client) {
			session, err := client.NewSession()
			if err != nil {
				t.Fatalf("session failed: %v", err)
			}
			defer session.Close()

			// Trigger a failure by attempting to execute a non-existent
			// command.
			err = session.Run(`non-existent command`)
			if err == nil {
				t.Fatalf("session succeeded: %v", err)
			}
		},
	}

	runTestAndUpdateIfNeeded(t, test.name, test.run)
}

func TestWindowChange(t *testing.T) {
	if runtime.GOARCH == "wasm" {
		t.Skip("skipping test, stdin/out are not supported on wasm")
	}
	test := clientTest{
		name:   "WindowChange",
		config: recordingsClientConfig(),
		successCallback: func(t *testing.T, client *ssh.Client) {
			session, err := client.NewSession()
			if err != nil {
				t.Fatalf("session failed: %v", err)
			}
			defer session.Close()

			stdout, err := session.StdoutPipe()
			if err != nil {
				t.Fatalf("unable to acquire stdout pipe: %s", err)
			}

			stdin, err := session.StdinPipe()
			if err != nil {
				t.Fatalf("unable to acquire stdin pipe: %s", err)
			}

			tm := ssh.TerminalModes{ssh.ECHO: 0}
			if err = session.RequestPty("xterm", 80, 40, tm); err != nil {
				t.Fatalf("req-pty failed: %s", err)
			}

			if err := session.WindowChange(100, 100); err != nil {
				t.Fatalf("window-change failed: %s", err)
			}

			err = session.Shell()
			if err != nil {
				t.Fatalf("session failed: %s", err)
			}

			stdin.Write([]byte("stty size && exit\n"))

			var buf bytes.Buffer
			if _, err := io.Copy(&buf, stdout); err != nil {
				t.Fatalf("reading failed: %s", err)
			}

			if sttyOutput := buf.String(); !strings.Contains(sttyOutput, "100 100") {
				t.Fatalf("terminal WindowChange failure: expected \"100 100\" stty output, got %s", sttyOutput)
			}
		},
	}

	runTestAndUpdateIfNeeded(t, test.name, test.run)
}
