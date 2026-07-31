// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"

	"golang.org/x/crypto/sha3"
	"golang.org/x/crypto/ssh"
)

const (
	defaultSSHDConfig = `
Protocol 2
Banner {{.Dir}}/banner
HostKey {{.Dir}}/id_rsa
HostKey {{.Dir}}/id_dsa
HostKey {{.Dir}}/id_ecdsa
HostCertificate {{.Dir}}/id_rsa-sha2-512-cert.pub
Pidfile {{.Dir}}/sshd.pid
KeyRegenerationInterval 3600
ServerKeyBits 768
SyslogFacility AUTH
LogLevel DEBUG2
LoginGraceTime 120
PermitRootLogin no
StrictModes no
RSAAuthentication yes
PubkeyAuthentication yes
AuthorizedKeysFile	{{.Dir}}/authorized_keys
TrustedUserCAKeys {{.Dir}}/id_ecdsa.pub
IgnoreRhosts yes
RhostsRSAAuthentication no
HostbasedAuthentication no
PubkeyAcceptedKeyTypes=*
# In recent versions of OpenSSH, Diffie-Hellman key exchange algorithms
# are disabled by default. However, they are still included in our default
# Key Exchange (KEX) configuration. We explicitly enable them here to
# maintain compatibility for our test cases.
KexAlgorithms +diffie-hellman-group14-sha1,diffie-hellman-group14-sha256,diffie-hellman-group14-sha256,diffie-hellman-group16-sha512,diffie-hellman-group-exchange-sha256
`
	multiAuthSshdConfigTail = `
UsePAM yes
PasswordAuthentication yes
ChallengeResponseAuthentication yes
AuthenticationMethods {{.AuthMethods}}
`
	maxAuthTriesSshdConfigTail = `
PasswordAuthentication yes
MaxAuthTries 1
`
)

var configTmpl = map[string]*template.Template{
	"default":      template.Must(template.New("").Parse(defaultSSHDConfig)),
	"MultiAuth":    template.Must(template.New("").Parse(defaultSSHDConfig + multiAuthSshdConfigTail)),
	"MaxAuthTries": template.Must(template.New("").Parse(defaultSSHDConfig + maxAuthTriesSshdConfigTail))}

type server struct {
	t          *testing.T
	configfile string

	testUser     string // test username for sshd
	testPasswd   string // test password for sshd
	sshdTestPwSo string // dynamic library to inject a custom password into sshd

	lastDialConn net.Conn
}

type storedHostKey struct {
	// keys map from an algorithm string to binary key data.
	keys map[string][]byte

	// checkCount counts the Check calls. Used for testing
	// rekeying.
	checkCount int
}

func (k *storedHostKey) Add(key ssh.PublicKey) {
	if k.keys == nil {
		k.keys = map[string][]byte{}
	}
	k.keys[key.Type()] = key.Marshal()
}

func (k *storedHostKey) Check(addr string, remote net.Addr, key ssh.PublicKey) error {
	k.checkCount++
	algo := key.Type()

	if k.keys == nil || !bytes.Equal(key.Marshal(), k.keys[algo]) {
		return fmt.Errorf("host key mismatch. Got %q, want %q", key, k.keys[algo])
	}
	return nil
}

func hostKeyDB() *storedHostKey {
	keyChecker := &storedHostKey{}
	keyChecker.Add(testPublicKeys["ecdsa"])
	keyChecker.Add(testPublicKeys["rsa"])
	keyChecker.Add(testPublicKeys["dsa"])
	return keyChecker
}

func clientConfig() *ssh.ClientConfig {
	config := &ssh.ClientConfig{
		User: username(),
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(testSigners["user"]),
		},
		HostKeyCallback: hostKeyDB().Check,
		HostKeyAlgorithms: []string{ // by default, don't allow certs as this affects the hostKeyDB checker
			ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSA, ssh.InsecureKeyAlgoDSA,
			ssh.KeyAlgoED25519,
		},
	}
	return config
}

// SSH reference tests run a connection against a reference implementation
// (OpenSSH) of SSH and record the bytes of the resulting connection. The Go
// code, during a test, is configured with deterministic randomness and so the
// reference test can be reproduced exactly in the future.
//
// In order to save everyone who wishes to run the tests from needing the
// reference implementation installed, the reference connections are saved in
// files in the testdata directory. Thus running the tests involves nothing
// external, but creating and updating them requires the reference
// implementation.
//
// Tests can be updated by running them with the -update flag. This will cause
// the test files for failing tests to be regenerated. Since the reference
// implementation will always generate fresh random numbers, large parts of the
// reference connection will always change.

var (
	update = flag.Bool("update", false, "update golden files on failure")
)

func runTestAndUpdateIfNeeded(t *testing.T, name string, run func(t *testing.T, update bool)) {
	success := t.Run(name, func(t *testing.T) {
		if !*update {
			t.Parallel()
		}
		run(t, false)
	})

	if !success && *update {
		t.Run(name+"#update", func(t *testing.T) {
			run(t, true)
		})
	}
}

// recordingConn is a net.Conn that records the traffic that passes through it.
// WriteTo can be used to produce output that can be later be loaded with
// ParseTestData.
type recordingConn struct {
	net.Conn
	clientToServer bool
	sync.Mutex
	flows   [][]byte
	reading bool
}

func (r *recordingConn) Read(b []byte) (n int, err error) {
	if n, err = r.Conn.Read(b); n == 0 {
		return
	}
	b = b[:n]

	r.Lock()
	defer r.Unlock()

	if l := len(r.flows); l == 0 || !r.reading {
		buf := make([]byte, len(b))
		copy(buf, b)
		r.flows = append(r.flows, buf)
	} else {
		r.flows[l-1] = append(r.flows[l-1], b[:n]...)
	}
	r.reading = true
	return
}

func (r *recordingConn) Write(b []byte) (n int, err error) {
	if n, err = r.Conn.Write(b); n == 0 {
		return
	}
	b = b[:n]

	r.Lock()
	defer r.Unlock()

	if l := len(r.flows); l == 0 || r.reading {
		buf := make([]byte, len(b))
		copy(buf, b)
		r.flows = append(r.flows, buf)
	} else {
		r.flows[l-1] = append(r.flows[l-1], b[:n]...)
	}
	r.reading = false
	return
}

// WriteTo writes Go source code to w that contains the recorded traffic.
func (r *recordingConn) WriteTo(w io.Writer) (int64, error) {
	var written int64
	for i, flow := range r.flows {
		source, dest := "client", "server"
		if !r.clientToServer {
			source, dest = dest, source
		}
		n, err := fmt.Fprintf(w, ">>> Flow %d (%s to %s)\n", i+1, source, dest)
		written += int64(n)
		if err != nil {
			return written, err
		}
		dumper := hex.Dumper(w)
		n, err = dumper.Write(flow)
		written += int64(n)
		if err != nil {
			return written, err
		}
		err = dumper.Close()
		if err != nil {
			return written, err
		}
		r.clientToServer = !r.clientToServer
	}
	return written, nil
}

func parseTestData(r io.Reader) (flows [][]byte, err error) {
	var currentFlow []byte

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		// If the line starts with ">>> " then it marks the beginning
		// of a new flow.
		if strings.HasPrefix(line, ">>> ") {
			if len(currentFlow) > 0 || len(flows) > 0 {
				flows = append(flows, currentFlow)
				currentFlow = nil
			}
			continue
		}

		// Otherwise the line is a line of hex dump that looks like:
		// 00000170  fc f5 06 bf (...)  |.....X{&?......!|
		// (Some bytes have been omitted from the middle section.)
		_, after, ok := strings.Cut(line, " ")
		if !ok {
			return nil, errors.New("invalid test data")
		}
		line = after

		before, _, ok := strings.Cut(line, "|")
		if !ok {
			return nil, errors.New("invalid test data")
		}
		line = before

		hexBytes := strings.Fields(line)
		for _, hexByte := range hexBytes {
			val, err := strconv.ParseUint(hexByte, 16, 8)
			if err != nil {
				return nil, errors.New("invalid hex byte in test data: " + err.Error())
			}
			currentFlow = append(currentFlow, byte(val))
		}
	}

	if len(currentFlow) > 0 {
		flows = append(flows, currentFlow)
	}

	return flows, nil
}

func newReplayingConn(t testing.TB, flows [][]byte) net.Conn {
	r := &replayingConn{
		t:       t,
		flows:   flows,
		reading: false,
	}
	r.readCond = sync.NewCond(&r.Mutex)
	return r
}

// replayingConn is a net.Conn that replays flows recorded by recordingConn.
type replayingConn struct {
	t testing.TB
	sync.Mutex
	flows   [][]byte
	reading bool
	// SSH channels use a read loop goroutine, we use this condition to wait
	// until we are ready to read/write.
	readCond *sync.Cond
}

var _ net.Conn = (*replayingConn)(nil)

func (r *replayingConn) Read(b []byte) (n int, err error) {
	r.Lock()
	defer r.Unlock()

	for !r.reading {
		r.readCond.Wait()
	}

	// Some tests run commands that return no output.
	if len(r.flows) == 0 {
		return 0, nil
	}

	n = copy(b, r.flows[0])
	r.flows[0] = r.flows[0][n:]
	if len(r.flows[0]) == 0 {
		r.flows = r.flows[1:]
		r.reading = false
		r.readCond.Broadcast()
		if len(r.flows) == 0 {
			return n, io.EOF
		}
	}
	return n, nil
}

func (r *replayingConn) Write(b []byte) (n int, err error) {
	r.Lock()
	defer r.Unlock()

	for r.reading {
		r.readCond.Wait()
	}

	if !bytes.HasPrefix(r.flows[0], b) {
		r.t.Errorf("write mismatch: expected %x, got %x", r.flows[0], b)
		r.reading = true
		r.readCond.Broadcast()
		return 0, fmt.Errorf("write mismatch")
	}
	r.flows[0] = r.flows[0][len(b):]
	if len(r.flows[0]) == 0 {
		r.flows = r.flows[1:]
		r.reading = true
		r.readCond.Broadcast()
	}
	return len(b), nil
}

func (r *replayingConn) Close() error {
	r.Lock()
	defer r.Unlock()

	if len(r.flows) > 0 {
		r.t.Errorf("closed with unfinished flows: %d", len(r.flows))
		return fmt.Errorf("unexpected close")
	}
	return nil
}

func (r *replayingConn) LocalAddr() net.Addr                { return nil }
func (r *replayingConn) RemoteAddr() net.Addr               { return nil }
func (r *replayingConn) SetDeadline(_ time.Time) error      { return nil }
func (r *replayingConn) SetReadDeadline(_ time.Time) error  { return nil }
func (r *replayingConn) SetWriteDeadline(_ time.Time) error { return nil }

func username() string {
	var username string
	if user, err := user.Current(); err == nil {
		username = user.Username
	} else {
		// user.Current() currently requires cgo. If an error is
		// returned attempt to get the username from the environment.
		log.Printf("user.Current: %v; falling back on $USER", err)
		username = os.Getenv("USER")
	}
	if username == "" {
		panic("Unable to get username")
	}
	return username
}

func writeFile(path string, contents []byte) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err := f.Write(contents); err != nil {
		panic(err)
	}
}

// setDeterministicRandomSource sets a deterministic random source for the
// provided ssh.Config. It is intended solely for use in test cases, as
// deterministic randomness is insecure and should never be used in production
// environments. A deterministic random source is required to enable consistent
// testing against recorded session files.
func setDeterministicRandomSource(config *ssh.Config) {
	config.Rand = sha3.NewShake128()
}

func TestMain(m *testing.M) {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args)
		flag.PrintDefaults()
	}

	flag.Parse()
	os.Exit(m.Run())
}
