package remote

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"reasonix/internal/proc"
)

const proxyCommandStderrLimit = 8 * 1024

type proxyCommandConn struct {
	cmd        *exec.Cmd
	job        uintptr
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     *proxyCommandStderr
	remoteAddr net.Addr
	done       chan struct{}

	closeOnce sync.Once
	jobOnce   sync.Once
	waitMu    sync.Mutex
	waitErr   error
}

func dialProxyCommand(ctx context.Context, host ResolvedHost) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command, err := expandProxyCommand(host.ProxyCommand, host)
	if err != nil {
		return nil, err
	}
	cmd := newProxyCommandProcess(command)
	proc.HideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("ProxyCommand stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("ProxyCommand stdout: %w", err)
	}
	stderr := &proxyCommandStderr{}
	cmd.Stderr = stderr
	job, err := proc.StartTracked(cmd)
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start ProxyCommand: %w", err)
	}
	conn := &proxyCommandConn{
		cmd:        cmd,
		job:        job,
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
		remoteAddr: proxyCommandAddr(host.Addr()),
		done:       make(chan struct{}),
	}
	go conn.wait()
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func expandProxyCommand(command string, host ResolvedHost) (string, error) {
	alias := host.SSHConfigAlias
	if alias == "" {
		alias = host.Name
	}
	tokens := map[byte]string{
		'%': "%",
		'h': host.HostName,
		'n': alias,
		'p': strconv.Itoa(host.Port),
		'r': host.User,
	}
	var expanded strings.Builder
	for i := 0; i < len(command); i++ {
		if command[i] != '%' {
			expanded.WriteByte(command[i])
			continue
		}
		i++
		if i == len(command) {
			return "", fmt.Errorf("ProxyCommand ends with incomplete token")
		}
		value, ok := tokens[command[i]]
		if !ok {
			return "", fmt.Errorf("ProxyCommand contains unsupported token %%%c", command[i])
		}
		if !validProxyCommandToken(command[i], value) {
			return "", fmt.Errorf("ProxyCommand token %%%c contains invalid characters", command[i])
		}
		expanded.WriteString(value)
	}
	return expanded.String(), nil
}

func validProxyCommandToken(token byte, value string) bool {
	switch token {
	case 'h', 'n':
		return validProxyCommandHostname(value)
	case 'r':
		return validProxyCommandUser(value)
	default:
		return true
	}
}

// These checks mirror OpenSSH's guards for values expanded into shell commands.
func validProxyCommandHostname(value string) bool {
	if strings.HasPrefix(value, "-") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("'`\"$\\;&<>|(){},", r) {
			return false
		}
	}
	return true
}

func validProxyCommandUser(value string) bool {
	if strings.HasPrefix(value, "-") {
		return false
	}
	runes := []rune(value)
	for i, r := range runes {
		if unicode.IsControl(r) || strings.ContainsRune("'`\";&<>|(){}", r) {
			return false
		}
		if unicode.IsSpace(r) && i+1 < len(runes) && runes[i+1] == '-' {
			return false
		}
	}
	return len(runes) == 0 || runes[len(runes)-1] != '\\'
}

func (c *proxyCommandConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *proxyCommandConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }

func (c *proxyCommandConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		_ = c.stdout.Close()
		select {
		case <-c.done:
			return
		default:
			proc.KillTracked(c.cmd, c.job)
			c.finishJob()
		}
		select {
		case <-c.done:
		case <-time.After(time.Second):
		}
	})
	return nil
}

func (c *proxyCommandConn) LocalAddr() net.Addr  { return proxyCommandAddr("proxy-command") }
func (c *proxyCommandConn) RemoteAddr() net.Addr { return c.remoteAddr }

func (c *proxyCommandConn) SetDeadline(time.Time) error      { return os.ErrNoDeadline }
func (c *proxyCommandConn) SetReadDeadline(time.Time) error  { return os.ErrNoDeadline }
func (c *proxyCommandConn) SetWriteDeadline(time.Time) error { return os.ErrNoDeadline }

func (c *proxyCommandConn) wait() {
	err := c.cmd.Wait()
	c.waitMu.Lock()
	c.waitErr = err
	c.waitMu.Unlock()
	c.finishJob()
	close(c.done)
}

func (c *proxyCommandConn) finishJob() {
	c.jobOnce.Do(func() { proc.FinishTracked(c.job) })
}

func (c *proxyCommandConn) failureDetail() string {
	stderr := strings.TrimSpace(c.stderr.String())
	select {
	case <-c.done:
		c.waitMu.Lock()
		err := c.waitErr
		c.waitMu.Unlock()
		if stderr != "" {
			return stderr
		}
		if err != nil {
			return err.Error()
		}
		return "process exited before the SSH handshake completed"
	default:
		return stderr
	}
}

func proxyCommandHandshakeError(conn net.Conn, err error) error {
	proxy, ok := conn.(*proxyCommandConn)
	if !ok {
		return err
	}
	if detail := proxy.failureDetail(); detail != "" {
		return fmt.Errorf("%w: ProxyCommand: %s", err, detail)
	}
	return err
}

type proxyCommandAddr string

func (a proxyCommandAddr) Network() string { return "proxy-command" }
func (a proxyCommandAddr) String() string  { return string(a) }

type proxyCommandStderr struct {
	mu   sync.Mutex
	data []byte
}

func (b *proxyCommandStderr) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.data = append(b.data, p...)
	if len(b.data) > proxyCommandStderrLimit {
		b.data = append([]byte(nil), b.data[len(b.data)-proxyCommandStderrLimit:]...)
	}
	b.mu.Unlock()
	return len(p), nil
}

func (b *proxyCommandStderr) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
