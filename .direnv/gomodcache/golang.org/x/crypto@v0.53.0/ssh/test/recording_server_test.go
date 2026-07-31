// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/internal/testenv"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
)

type serverTest struct {
	// name is a freeform string identifying the test and the file in which
	// the expected results will be stored.
	name string
	// config contains the server configuration to use for this test.
	config *ssh.ServerConfig
}

// connFromCommand starts opens a listening socket and starts the reference
// client to connect to it. It returns a recordingConn that wraps the resulting
// connection.
func (test *serverTest) connFromCommand(t *testing.T) (conn *recordingConn, err error) {
	sshCLI, err := exec.LookPath("ssh")
	if err != nil {
		t.Skipf("skipping test: %v", err)
	}
	l, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	})
	if err != nil {
		return nil, err
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port
	dir, err := os.MkdirTemp("", "sshtest")
	if err != nil {
		t.Fatal(err)
	}

	filename := "id_ed25519"
	writeFile(filepath.Join(dir, filename), testdata.PEMBytes["ed25519"])
	writeFile(filepath.Join(dir, filename+".pub"), ssh.MarshalAuthorizedKey(testPublicKeys["ed25519"]))
	var args []string
	args = append(args, "-v", "-i", filepath.Join(dir, filename), "-o", "StrictHostKeyChecking=no")
	args = append(args, "-oKexAlgorithms=+diffie-hellman-group14-sha1")
	args = append(args, "-p", strconv.Itoa(port))
	args = append(args, "testuser@127.0.0.1")
	args = append(args, "true")
	cmd := testenv.Command(t, sshCLI, args...)
	cmd.Stdin = nil
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
		// Don't check for errors; if it fails it's most
		// likely "os: process already finished", and we don't
		// care about that.
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("OpenSSH output:\n\n%s", cmd.Stdout)
		}
	})

	connChan := make(chan any, 1)
	go func() {
		tcpConn, err := l.Accept()
		if err != nil {
			connChan <- err
			return
		}
		connChan <- tcpConn
	}()

	var tcpConn net.Conn
	select {
	case connOrError := <-connChan:
		if err, ok := connOrError.(error); ok {
			return nil, err
		}
		tcpConn = connOrError.(net.Conn)
	case <-time.After(2 * time.Second):
		return nil, errors.New("timed out waiting for connection from child process")
	}

	record := &recordingConn{
		Conn:           tcpConn,
		clientToServer: false,
	}

	return record, nil
}

func (test *serverTest) dataPath() string {
	return filepath.Join("..", "testdata", "Server-"+test.name)
}

func (test *serverTest) loadData() (flows [][]byte, err error) {
	in, err := os.Open(test.dataPath())
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return parseTestData(in)
}

func (test *serverTest) run(t *testing.T, write bool) {
	var serverConn net.Conn
	var recordingConn *recordingConn

	setDeterministicRandomSource(&test.config.Config)

	if write {
		var err error
		recordingConn, err = test.connFromCommand(t)
		if err != nil {
			t.Fatalf("Failed to start subcommand: %v", err)
		}
		serverConn = recordingConn
	} else {
		timer := time.AfterFunc(10*time.Second, func() {
			fmt.Println("This test may be stuck, try running using -timeout 10s")
		})
		t.Cleanup(func() {
			timer.Stop()
		})
		flows, err := test.loadData()
		if err != nil {
			t.Fatalf("Failed to load data from %s", test.dataPath())
		}
		serverConn = newReplayingConn(t, flows)
	}

	server, chans, reqs, err := ssh.NewServerConn(serverConn, test.config)
	if err != nil {
		t.Fatalf("Failed to create server conn: %v", err)
	}
	defer server.Close()

	go ssh.DiscardRequests(reqs)

	done := make(chan bool)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go func(in <-chan *ssh.Request) {
			for req := range in {
				switch req.Type {
				case "exec":
					if req.WantReply {
						req.Reply(true, nil)
					}
					channel.SendRequest("exit-status", false, ssh.Marshal(&exitStatusMsg{Status: 0}))
					channel.Close()
					done <- true
				default:
					if req.WantReply {
						req.Reply(false, nil)
					}
				}
			}
		}(requests)
	}

	<-done

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

func recordingsServerConfig() *ssh.ServerConfig {
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	config.SetDefaults()
	// Remove ML-KEM since it only works with Go 1.24.
	config.SetDefaults()
	if config.KeyExchanges[0] == ssh.KeyExchangeMLKEM768X25519 {
		config.KeyExchanges = config.KeyExchanges[1:]
	}
	config.AddHostKey(testSigners["rsa"])
	return config
}

func TestServerKeyExchanges(t *testing.T) {
	config := ssh.ClientConfig{}
	config.SetDefaults()

	var keyExchanges []string
	for _, kex := range config.KeyExchanges {
		// Exclude ecdh for now, to make them deterministic we should use see a
		// stream of fixed bytes as the random source.
		// Exclude ML-KEM because server side is not deterministic.
		if !strings.HasPrefix(kex, "ecdh-") && !strings.HasPrefix(kex, "mlkem") {
			keyExchanges = append(keyExchanges, kex)
		}
	}
	// Add diffie-hellman-group16-sha512 as it is not enabled by default.
	keyExchanges = append(keyExchanges, "diffie-hellman-group16-sha512")

	for _, kex := range keyExchanges {
		c := recordingsServerConfig()
		c.KeyExchanges = []string{kex}
		test := serverTest{
			name:   "KEX-" + kex,
			config: c,
		}
		runTestAndUpdateIfNeeded(t, test.name, test.run)
	}
}

func TestServerCiphers(t *testing.T) {
	config := ssh.ClientConfig{}
	config.SetDefaults()

	for _, ciph := range config.Ciphers {
		c := recordingsServerConfig()
		c.Ciphers = []string{ciph}
		test := serverTest{
			name:   "Cipher-" + ciph,
			config: c,
		}
		runTestAndUpdateIfNeeded(t, test.name, test.run)
	}
}

func TestServerMACs(t *testing.T) {
	config := ssh.ClientConfig{}
	config.SetDefaults()

	for _, mac := range config.MACs {
		c := recordingsServerConfig()
		c.MACs = []string{mac}
		test := serverTest{
			name:   "MAC-" + mac,
			config: c,
		}
		runTestAndUpdateIfNeeded(t, test.name, test.run)
	}
}
