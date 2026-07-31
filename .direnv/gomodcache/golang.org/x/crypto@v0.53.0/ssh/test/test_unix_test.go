// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || plan9 || solaris

package test

// functional test harness for unix.

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/internal/testenv"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
)

// unixConnection creates two halves of a connected net.UnixConn.  It
// is used for connecting the Go SSH client with sshd without opening
// ports.
func unixConnection() (*net.UnixConn, *net.UnixConn, error) {
	dir, err := os.MkdirTemp("", "unixConnection")
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(dir)

	addr := filepath.Join(dir, "ssh")
	listener, err := net.Listen("unix", addr)
	if err != nil {
		return nil, nil, err
	}
	defer listener.Close()
	c1, err := net.Dial("unix", addr)
	if err != nil {
		return nil, nil, err
	}

	c2, err := listener.Accept()
	if err != nil {
		c1.Close()
		return nil, nil, err
	}

	return c1.(*net.UnixConn), c2.(*net.UnixConn), nil
}

func (s *server) TryDial(config *ssh.ClientConfig) (*ssh.Client, error) {
	return s.TryDialWithAddr(config, "")
}

// addr is the user specified host:port. While we don't actually dial it,
// we need to know this for host key matching
func (s *server) TryDialWithAddr(config *ssh.ClientConfig, addr string) (client *ssh.Client, err error) {
	sshd, err := exec.LookPath("sshd")
	if err != nil {
		s.t.Skipf("skipping test: %v", err)
	}

	c1, c2, err := unixConnection()
	if err != nil {
		s.t.Fatalf("unixConnection: %v", err)
	}
	defer func() {
		// Close c2 after we've started the sshd command so that it won't prevent c1
		// from returning EOF when the sshd command exits.
		c2.Close()

		// Leave c1 open if we're returning a client that wraps it.
		// (The client is responsible for closing it.)
		// Otherwise, close it to free up the socket.
		if client == nil {
			c1.Close()
		}
	}()

	f, err := c2.File()
	if err != nil {
		s.t.Fatalf("UnixConn.File: %v", err)
	}
	defer f.Close()

	cmd := testenv.Command(s.t, sshd, "-f", s.configfile, "-i", "-e")
	cmd.Stdin = f
	cmd.Stdout = f
	cmd.Stderr = new(bytes.Buffer)

	if s.sshdTestPwSo != "" {
		if s.testUser == "" {
			s.t.Fatal("user missing from sshd_test_pw.so config")
		}
		if s.testPasswd == "" {
			s.t.Fatal("password missing from sshd_test_pw.so config")
		}
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("LD_PRELOAD=%s", s.sshdTestPwSo),
			fmt.Sprintf("TEST_USER=%s", s.testUser),
			fmt.Sprintf("TEST_PASSWD=%s", s.testPasswd))
	}

	if err := cmd.Start(); err != nil {
		s.t.Fatalf("s.cmd.Start: %v", err)
	}
	s.lastDialConn = c1
	s.t.Cleanup(func() {
		// Don't check for errors; if it fails it's most
		// likely "os: process already finished", and we don't
		// care about that. Use os.Interrupt, so child
		// processes are killed too.
		cmd.Process.Signal(os.Interrupt)
		cmd.Wait()
		if s.t.Failed() || testing.Verbose() {
			// log any output from sshd process
			s.t.Logf("sshd:\n%s", cmd.Stderr)
		}
	})

	conn, chans, reqs, err := ssh.NewClientConn(c1, addr, config)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(conn, chans, reqs), nil
}

func (s *server) Dial(config *ssh.ClientConfig) *ssh.Client {
	conn, err := s.TryDial(config)
	if err != nil {
		s.t.Fatalf("ssh.Client: %v", err)
	}
	return conn
}

// generate random password
func randomPassword() (string, error) {
	b := make([]byte, 12)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// setTestPassword is used for setting user and password data for sshd_test_pw.so
// This function also checks that ./sshd_test_pw.so exists and if not calls s.t.Skip()
func (s *server) setTestPassword(user, passwd string) error {
	wd, _ := os.Getwd()
	wrapper := filepath.Join(wd, "sshd_test_pw.so")
	if _, err := os.Stat(wrapper); err != nil {
		s.t.Skip(fmt.Errorf("sshd_test_pw.so is not available"))
		return err
	}

	s.sshdTestPwSo = wrapper
	s.testUser = user
	s.testPasswd = passwd
	return nil
}

// newServer returns a new mock ssh server.
func newServer(t *testing.T) *server {
	return newServerForConfig(t, "default", map[string]string{})
}

// newServerForConfig returns a new mock ssh server.
func newServerForConfig(t *testing.T, config string, configVars map[string]string) *server {
	if testing.Short() {
		t.Skip("skipping test due to -short")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}
	uname := u.Name
	if uname == "" {
		// Check the value of u.Username as u.Name
		// can be "" on some OSes like AIX.
		uname = u.Username
	}
	if uname == "root" {
		t.Skip("skipping test because current user is root")
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
	configVars["Dir"] = dir
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

	for k, v := range testdata.SSHCertificates {
		filename := "id_" + k + "-cert.pub"
		writeFile(filepath.Join(dir, filename), v)
	}

	var authkeys bytes.Buffer
	for k := range testdata.PEMBytes {
		authkeys.Write(ssh.MarshalAuthorizedKey(testPublicKeys[k]))
	}
	writeFile(filepath.Join(dir, "authorized_keys"), authkeys.Bytes())
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})

	return &server{
		t:          t,
		configfile: f.Name(),
	}
}

func newTempSocket(t *testing.T) (string, func()) {
	dir, err := os.MkdirTemp("", "socket")
	if err != nil {
		t.Fatal(err)
	}
	deferFunc := func() { os.RemoveAll(dir) }
	addr := filepath.Join(dir, "sock")
	return addr, deferFunc
}
