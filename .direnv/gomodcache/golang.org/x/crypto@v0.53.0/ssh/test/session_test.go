// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows && !js && !wasip1

package test

// Session functional tests.

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func skipIfIssue64959(t *testing.T, err error) {
	if err != nil && runtime.GOOS == "darwin" && strings.Contains(err.Error(), "ssh: unexpected packet in response to channel open: <nil>") {
		t.Helper()
		t.Skipf("skipping test broken on some versions of macOS; see https://go.dev/issue/64959")
	}
}

func TestRunCommandWeClosed(t *testing.T) {
	server := newServer(t)
	conn := server.Dial(clientConfig())
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		skipIfIssue64959(t, err)
		t.Fatalf("session failed: %v", err)
	}
	err = session.Shell()
	if err != nil {
		t.Fatalf("shell failed: %v", err)
	}
	err = session.Close()
	if err != nil {
		t.Fatalf("shell failed: %v", err)
	}
}

func TestFuncLargeRead(t *testing.T) {
	server := newServer(t)
	conn := server.Dial(clientConfig())
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		skipIfIssue64959(t, err)
		t.Fatalf("unable to create new session: %s", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		t.Fatalf("unable to acquire stdout pipe: %s", err)
	}

	err = session.Start("dd if=/dev/urandom bs=2048 count=1024")
	if err != nil {
		t.Fatalf("unable to execute remote command: %s", err)
	}

	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, stdout)
	if err != nil {
		t.Fatalf("error reading from remote stdout: %s", err)
	}

	if n != 2048*1024 {
		t.Fatalf("Expected %d bytes but read only %d from remote command", 2048, n)
	}
}

func TestKeyChange(t *testing.T) {
	server := newServer(t)
	conf := clientConfig()
	hostDB := hostKeyDB()
	conf.HostKeyCallback = hostDB.Check
	conf.RekeyThreshold = 1024
	conn := server.Dial(conf)
	defer conn.Close()

	for i := 0; i < 4; i++ {
		session, err := conn.NewSession()
		if err != nil {
			skipIfIssue64959(t, err)
			t.Fatalf("unable to create new session: %s", err)
		}

		stdout, err := session.StdoutPipe()
		if err != nil {
			t.Fatalf("unable to acquire stdout pipe: %s", err)
		}

		err = session.Start("dd if=/dev/urandom bs=1024 count=1")
		if err != nil {
			t.Fatalf("unable to execute remote command: %s", err)
		}
		buf := new(bytes.Buffer)
		n, err := io.Copy(buf, stdout)
		if err != nil {
			t.Fatalf("error reading from remote stdout: %s", err)
		}

		want := int64(1024)
		if n != want {
			t.Fatalf("Expected %d bytes but read only %d from remote command", want, n)
		}
	}

	if changes := hostDB.checkCount; changes < 4 {
		t.Errorf("got %d key changes, want 4", changes)
	}
}

func TestValidTerminalMode(t *testing.T) {
	if runtime.GOOS == "aix" {
		// On AIX, sshd cannot acquire /dev/pts/* if launched as
		// a non-root user.
		t.Skipf("skipping on %s", runtime.GOOS)
	}
	server := newServer(t)
	conn := server.Dial(clientConfig())
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		skipIfIssue64959(t, err)
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

	err = session.Shell()
	if err != nil {
		t.Fatalf("session failed: %s", err)
	}

	if _, err := io.WriteString(stdin, "echo && echo SHELL $SHELL && stty -a && exit\n"); err != nil {
		t.Fatal(err)
	}

	buf := new(strings.Builder)
	if _, err := io.Copy(buf, stdout); err != nil {
		t.Fatalf("reading failed: %s", err)
	}

	if testing.Verbose() {
		t.Logf("echo && echo SHELL $SHELL && stty -a && exit:\n%s", buf)
	}

	shellLine := regexp.MustCompile("(?m)^SHELL (.*)$").FindStringSubmatch(buf.String())
	if len(shellLine) != 2 {
		t.Fatalf("missing output from echo SHELL $SHELL")
	}
	switch shell := filepath.Base(strings.TrimSpace(shellLine[1])); shell {
	case "sh", "bash":
	default:
		t.Skipf("skipping test on non-Bourne shell %q", shell)
	}

	if sttyOutput := buf.String(); !strings.Contains(sttyOutput, "-echo ") {
		t.Fatal("terminal mode failure: expected -echo in stty output")
	}
}

func testOneCipher(t *testing.T, cipher string, cipherOrder []string) {
	server := newServer(t)
	conf := clientConfig()
	conf.Ciphers = []string{cipher}
	// Don't fail if sshd doesn't have the cipher.
	conf.Ciphers = append(conf.Ciphers, cipherOrder...)
	conn, err := server.TryDial(conf)
	if err != nil {
		t.Fatalf("TryDial: %v", err)
	}
	defer conn.Close()

	numBytes := 4096

	// Exercise receiving data from the server
	session, err := conn.NewSession()
	if err != nil {
		skipIfIssue64959(t, err)
		t.Fatalf("NewSession: %v", err)
	}

	out, err := session.Output(fmt.Sprintf("dd if=/dev/zero bs=%d count=1", numBytes))
	if err != nil {
		t.Fatalf("Output: %v", err)
	}

	if len(out) != numBytes {
		t.Fatalf("got %d bytes, want %d bytes", len(out), numBytes)
	}

	// Exercise sending data to the server
	if _, _, err := conn.Conn.SendRequest("drop-me", false, make([]byte, numBytes)); err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
}

var deprecatedCiphers = []string{
	ssh.InsecureCipherAES128CBC, ssh.InsecureCipherTripleDESCBC,
	ssh.InsecureCipherRC4128, ssh.InsecureCipherRC4256,
}

func TestCiphers(t *testing.T) {
	var config ssh.Config
	config.SetDefaults()
	cipherOrder := append(config.Ciphers, deprecatedCiphers...)

	for _, ciph := range cipherOrder {
		t.Run(ciph, func(t *testing.T) {
			testOneCipher(t, ciph, cipherOrder)
		})
	}
}

func TestClientAuthAlgorithms(t *testing.T) {
	for _, key := range []string{
		"rsa",
		"ecdsa",
		"ed25519",
	} {
		t.Run(key, func(t *testing.T) {
			server := newServer(t)
			conf := clientConfig()
			conf.SetDefaults()
			conf.Auth = []ssh.AuthMethod{
				ssh.PublicKeys(testSigners[key]),
			}

			conn, err := server.TryDial(conf)
			if err == nil {
				conn.Close()
			} else {
				t.Errorf("failed for key %q", key)
			}
		})
	}
}

func TestClientAuthDisconnect(t *testing.T) {
	// Use a static key that is not accepted by server.
	// This key has been generated with following ssh-keygen command and
	// used exclusively in this unit test:
	// $ ssh-keygen -t RSA -b 2048 -f /tmp/static_key \
	//   -C "Static RSA key for golang.org/x/crypto/ssh unit test"

	const privKeyData = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn
NhAAAAAwEAAQAAAQEAwV1Zg3MqX27nIQQNWd8V09P4q4F1fx7H2xNJdL3Yg3y91GFLJ92+
0IiGV8n1VMGL/71PPhzyqBpUYSTpWjiU2JZSfA+iTg1GJBcOaEOA6vrXsTtXTHZ//mOT4d
mlvuP4+9NqfCBLGXN7ZJpT+amkD8AVW9YW9QN3ipY61ZWxPaAocVpDd8rVgJTk54KvaPa7
t4ddOSQDQq61aubIDR1Z3P+XjkB4piWOsbck3HJL+veTALy12C09tAhwUnZUAXS+DjhxOL
xpDVclF/yXYhAvBvsjwyk/OC3+nK9F799hpQZsjxmbP7oN+tGwz06BUcAKi7u7QstENvvk
85SDZy1q1QAAA/A7ylbJO8pWyQAAAAdzc2gtcnNhAAABAQDBXVmDcypfbuchBA1Z3xXT0/
irgXV/HsfbE0l0vdiDfL3UYUsn3b7QiIZXyfVUwYv/vU8+HPKoGlRhJOlaOJTYllJ8D6JO
DUYkFw5oQ4Dq+texO1dMdn/+Y5Ph2aW+4/j702p8IEsZc3tkmlP5qaQPwBVb1hb1A3eKlj
rVlbE9oChxWkN3ytWAlOTngq9o9ru3h105JANCrrVq5sgNHVnc/5eOQHimJY6xtyTcckv6
95MAvLXYLT20CHBSdlQBdL4OOHE4vGkNVyUX/JdiEC8G+yPDKT84Lf6cr0Xv32GlBmyPGZ
s/ug360bDPToFRwAqLu7tCy0Q2++TzlINnLWrVAAAAAwEAAQAAAQAIvPDHMiyIxgCksGPF
uyv9F9U4XjVip8/abE9zkAMJWW5++wuT/bRlBOUPRrWIXZEM9ETbtsqswo3Wxah+7CjRIH
qR7SdFlYTP1jPk4yIKXF4OvggBUPySkMpAGJ9hwOMW8Ymcb4gn77JJ4aMoWIcXssje+XiC
8iO+4UWU3SV2i6K7flK1UDCI5JVCyBr3DVf3QhMOgvwJl9TgD7FzWy1hkjuZq/Pzdv+fA2
OfrUFiSukLNolidNoI9+KWa1yxixE+B2oN4Xan3ZbqGbL6Wc1dB+K9h/bNcu+SKf7fXWRi
/vVG44A61xGDZzen1+eQlqFp7narkKXoaU71+45VXDThAAAAgBPWUdQykEEm0yOS6hPIW+
hS8z1LXWGTEcag9fMwJXKE7cQFO3LEk+dXMbClHdhD/ydswOZYGSNepxwvmo/a5LiO2ulp
W+5tnsNhcK3skdaf71t+boUEXBNZ6u3WNTkU7tDN8h9tebI+xlNceDGSGjOlNoHQVMKZdA
W9TA4ZqXUPAAAAgQDWU0UZVOSCAOODPz4PYsbFKdCfXNP8O4+t9txyc9E3hsLAsVs+CpVX
Gr219MGLrublzAxojipyzuQb6Tp1l9nsu7VkcBrPL8I1tokz0AyTnmNF3A9KszBal7gGNS
a2qYuf6JO4cub1KzonxUJQHZPZq9YhCxOtDwTd+uyHZiPy9QAAAIEA5vayd+nfVJgCKTdf
z5MFsxBSUj/cAYg7JYPS/0bZ5bEkLosL22wl5Tm/ZftJa8apkyBPhguAWt6jEWLoDiK+kn
Fv0SaEq1HUdXgWmISVnWzv2pxdAtq/apmbxTg3iIJyrAwEDo13iImR3k6rNPx1m3i/jX56
HLcvWM4Y6bFzbGEAAAA0U3RhdGljIFJTQSBrZXkgZm9yIGdvbGFuZy5vcmcveC9jcnlwdG
8vc3NoIHVuaXQgdGVzdAECAwQFBgc=
-----END OPENSSH PRIVATE KEY-----`

	signer, err := ssh.ParsePrivateKey([]byte(privKeyData))
	if err != nil {
		t.Fatalf("failed to create signer from key: %v", err)
	}

	// Start server with MaxAuthTries 1 and publickey and password auth
	// enabled
	server := newServerForConfig(t, "MaxAuthTries", map[string]string{})

	// Connect to server, expect failure, that PublicKeysCallback is called
	// and that PasswordCallback is not called.
	publicKeysCallbackCalled := false
	config := clientConfig()
	config.Auth = []ssh.AuthMethod{
		ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			publicKeysCallbackCalled = true
			return []ssh.Signer{signer}, nil
		}),
		ssh.PasswordCallback(func() (string, error) {
			t.Errorf("unexpected call to PasswordCallback()")
			return "notaverygoodpassword", nil
		}),
	}
	client, err := server.TryDial(config)
	if err == nil {
		t.Errorf("expected TryDial() to fail")
		_ = client.Close()
	}
	if !publicKeysCallbackCalled {
		t.Errorf("expected PublicKeysCallback() to be called")
	}
}
