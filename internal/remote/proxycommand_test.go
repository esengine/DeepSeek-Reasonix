package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/remote/sshtest"
)

const proxyCommandHelperEnv = "REASONIX_PROXY_COMMAND_HELPER"

func TestExpandProxyCommand(t *testing.T) {
	host := ResolvedHost{
		Name: "display", HostName: "resolved.example", Port: 2207, User: "dev",
		SSHConfigAlias: "original-alias",
	}
	got, err := expandProxyCommand("proxy --host %h --original %n --port %p --user %r --percent %%", host)
	if err != nil {
		t.Fatal(err)
	}
	want := "proxy --host resolved.example --original original-alias --port 2207 --user dev --percent %"
	if got != want {
		t.Fatalf("expanded ProxyCommand = %q, want %q", got, want)
	}
	for _, command := range []string{"proxy %x", "proxy %"} {
		if _, err := expandProxyCommand(command, host); err == nil {
			t.Fatalf("expandProxyCommand(%q) accepted an invalid token", command)
		}
	}
}

func TestExpandProxyCommandRejectsUnsafeTokens(t *testing.T) {
	tests := []struct {
		name    string
		command string
		host    ResolvedHost
	}{
		{name: "hostname", command: "proxy %h", host: ResolvedHost{HostName: "host;calc", Port: 22, User: "dev", SSHConfigAlias: "alias"}},
		{name: "original host", command: "proxy %n", host: ResolvedHost{HostName: "host", Port: 22, User: "dev", SSHConfigAlias: "alias|calc"}},
		{name: "user", command: "proxy %r", host: ResolvedHost{HostName: "host", Port: 22, User: "dev&calc", SSHConfigAlias: "alias"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := expandProxyCommand(tt.command, tt.host); err == nil {
				t.Fatalf("expandProxyCommand(%q) accepted unsafe host %+v", tt.command, tt.host)
			}
		})
	}
}

func TestExpandProxyCommandAcceptsOpenSSHHostAndUserForms(t *testing.T) {
	host := ResolvedHost{
		Name: "display", HostName: "fe80::1%12", Port: 22, User: `DOMAIN\dev user`,
		SSHConfigAlias: "host.example",
	}
	got, err := expandProxyCommand("proxy %h %n %p %r", host)
	if err != nil {
		t.Fatal(err)
	}
	if got != `proxy fe80::1%12 host.example 22 DOMAIN\dev user` {
		t.Fatalf("expanded ProxyCommand = %q", got)
	}
}

func TestProxyCommandConnectsSSHTransport(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "hunter2"})
	t.Setenv(proxyCommandHelperEnv, "1")
	host := ResolvedHost{
		Name: "friendly", HostName: "proxy-target.invalid", Port: 2207, User: "test",
		SSHConfigAlias: "saved-alias",
		ProxyCommand:   proxyCommandHelperCommand("bridge", srv.Addr) + " %h %n %p %r %%",
	}
	client, err := New(Options{
		Host: host,
		Auth: AuthOptions{DisableAgent: true, Password: func() (string, error) {
			return "hunter2", nil
		}},
		HostKeys: managedOnlyPolicy(t, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start through ProxyCommand: %v", err)
	}
	defer client.Close()
	result, err := client.Exec(ctx, "echo proxy-command")
	if err != nil {
		t.Fatalf("Exec through ProxyCommand: %v", err)
	}
	if strings.TrimSpace(string(result.Stdout)) != "echo proxy-command" {
		t.Fatalf("Exec stdout = %q", result.Stdout)
	}
}

func TestProxyCommandStderrIsReported(t *testing.T) {
	t.Setenv(proxyCommandHelperEnv, "1")
	host := ResolvedHost{
		Name: "broken", HostName: "proxy-target.invalid", Port: 22, User: "test",
		ProxyCommand: proxyCommandHelperCommand("fail"),
	}
	client, err := New(Options{
		Host: host, Auth: AuthOptions{DisableAgent: true}, HostKeys: managedOnlyPolicy(t, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = client.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "proxy helper failed") {
		t.Fatalf("ProxyCommand failure = %v", err)
	}
}

func TestProxyCommandStderrDoesNotReclassifyTransportFailure(t *testing.T) {
	t.Setenv(proxyCommandHelperEnv, "1")
	host := ResolvedHost{
		Name: "broken", HostName: "proxy-target.invalid", Port: 22, User: "test",
		ProxyCommand: proxyCommandHelperCommand("permission-denied"),
	}
	client, err := New(Options{
		Host: host, Auth: AuthOptions{DisableAgent: true}, HostKeys: managedOnlyPolicy(t, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = client.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("ProxyCommand failure = %v", err)
	}
	if errors.Is(err, ErrAuthFailed) {
		t.Fatalf("ProxyCommand transport failure was classified as SSH authentication: %v", err)
	}
}

func TestProxyCommandDetailPreservesAuthenticationClassification(t *testing.T) {
	proxy := &proxyCommandConn{stderr: &proxyCommandStderr{}}
	_, _ = proxy.stderr.Write([]byte("proxy diagnostic"))
	authErr := classifyDialError(errors.New("ssh: unable to authenticate"))
	err := proxyCommandHandshakeError(proxy, authErr)
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("wrapped authentication failure = %v, want ErrAuthFailed", err)
	}
}

func TestProxyCommandHelperProcess(t *testing.T) {
	if os.Getenv(proxyCommandHelperEnv) != "1" {
		return
	}
	runProxyCommandHelper()
}

func proxyCommandHelperCommand(args ...string) string {
	parts := []string{shellQuoteProxyCommandArg(os.Args[0]), "-test.run=^TestProxyCommandHelperProcess$"}
	parts = append(parts, "--")
	for _, arg := range args {
		parts = append(parts, shellQuoteProxyCommandArg(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuoteProxyCommandArg(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runProxyCommandHelper() {
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "proxy helper missing mode")
		os.Exit(2)
	}
	args = args[1:]
	switch args[0] {
	case "fail":
		fmt.Fprintln(os.Stderr, "proxy helper failed")
		os.Exit(42)
	case "permission-denied":
		fmt.Fprintln(os.Stderr, "permission denied")
		os.Exit(43)
	case "bridge":
		if len(args) != 7 || args[2] != "proxy-target.invalid" || args[3] != "saved-alias" || args[4] != "2207" || args[5] != "test" || args[6] != "%" {
			fmt.Fprintf(os.Stderr, "unexpected proxy helper args: %q\n", args)
			os.Exit(2)
		}
		conn, err := net.Dial("tcp", args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		go func() {
			_, _ = io.Copy(conn, os.Stdin)
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.CloseWrite()
			}
		}()
		_, _ = io.Copy(os.Stdout, conn)
		_ = conn.Close()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown proxy helper mode: %s\n", args[0])
		os.Exit(2)
	}
}
