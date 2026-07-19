package remote

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// HostKeyQuestion describes a first-seen (TOFU) host key awaiting the user's
// decision.
type HostKeyQuestion struct {
	Host        string // display label (user@host:port or alias)
	Address     string // the network address that presented the key
	KeyType     string // e.g. "ssh-ed25519"
	Fingerprint string // ssh.FingerprintSHA256(key)
}

// HostKeyPrompt is called for an unknown host key. Returning (true, nil)
// accepts and persists it (trust on first use); (false, nil) rejects; a
// non-nil error aborts the dial. A nil prompt means strict mode: unknown hosts
// are rejected.
type HostKeyPrompt func(ctx context.Context, q HostKeyQuestion) (accept bool, err error)

// HostKeyPolicy verifies presented host keys against the user's OpenSSH
// known_hosts files (read-only) and a Reasonix-managed file (read-write, TOFU).
type HostKeyPolicy struct {
	// SystemKnownHosts are OpenSSH known_hosts files consulted read-only.
	// Empty => [~/.ssh/known_hosts, ~/.ssh/known_hosts2] when they exist.
	SystemKnownHosts []string
	// ManagedPath is the Reasonix-managed known_hosts file that accepted TOFU
	// keys are appended to. Empty => config.RemoteKnownHostsPath().
	ManagedPath string
	// Prompt decides unknown (first-seen) keys. Nil => strict reject.
	Prompt HostKeyPrompt

	mu sync.Mutex // serializes appends to ManagedPath
}

// Callback builds an ssh.HostKeyCallback enforcing this policy for host (the
// display label used in prompts). ctx bounds any interactive prompt.
func (p *HostKeyPolicy) Callback(ctx context.Context, host string) (ssh.HostKeyCallback, error) {
	files := p.systemFiles()
	managed := p.managedPath()
	if managed != "" {
		if err := os.MkdirAll(filepath.Dir(managed), 0o700); err != nil {
			return nil, err
		}
		// knownhosts.New requires each file to exist; create an empty managed
		// file on first use.
		if _, err := os.Stat(managed); os.IsNotExist(err) {
			if err := os.WriteFile(managed, nil, 0o600); err != nil {
				return nil, err
			}
		}
		files = append(files, managed)
	}

	var base ssh.HostKeyCallback
	if len(files) > 0 {
		var err error
		base, err = knownhosts.New(files...)
		if err != nil {
			return nil, fmt.Errorf("load known_hosts: %w", err)
		}
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if base != nil {
			err := base(hostname, remote, key)
			if err == nil {
				return nil
			}
			var keyErr *knownhosts.KeyError
			if !asKeyError(err, &keyErr) {
				return err
			}
			if len(keyErr.Want) > 0 {
				// A different key is on record for this host: hard fail, never
				// promptable. Name the file:line so the user can inspect it.
				return fmt.Errorf("%w for %s: presented %s; known_hosts records a different key%s",
					ErrHostKeyMismatch, host, ssh.FingerprintSHA256(key), knownHostsLocations(keyErr))
			}
			// len(Want)==0 => host unknown. Fall through to TOFU.
		}
		return p.tofu(ctx, host, hostname, remote, key, managed)
	}, nil
}

func (p *HostKeyPolicy) tofu(ctx context.Context, host, hostname string, remote net.Addr, key ssh.PublicKey, managed string) error {
	if p.Prompt == nil {
		return fmt.Errorf("%w for %s: unknown host key %s (no confirmation available)",
			ErrHostKeyRejected, host, ssh.FingerprintSHA256(key))
	}
	accept, err := p.Prompt(ctx, HostKeyQuestion{
		Host:        host,
		Address:     remote.String(),
		KeyType:     key.Type(),
		Fingerprint: ssh.FingerprintSHA256(key),
	})
	if err != nil {
		return err
	}
	if !accept {
		return fmt.Errorf("%w for %s", ErrHostKeyRejected, host)
	}
	if managed == "" {
		return nil // accepted for this session only
	}
	return p.appendManaged(managed, hostname, remote, key)
}

func (p *HostKeyPolicy) appendManaged(managed, hostname string, remote net.Addr, key ssh.PublicKey) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	addrs := []string{knownhosts.Normalize(hostname)}
	if remote != nil {
		if norm := knownhosts.Normalize(remote.String()); norm != addrs[0] {
			addrs = append(addrs, norm)
		}
	}
	line := knownhosts.Line(addrs, key)
	f, err := os.OpenFile(managed, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(strings.TrimRight(line, "\n") + "\n"); err != nil {
		return err
	}
	return nil
}

func (p *HostKeyPolicy) systemFiles() []string {
	if len(p.SystemKnownHosts) > 0 {
		out := make([]string, 0, len(p.SystemKnownHosts))
		for _, f := range p.SystemKnownHosts {
			if f = expandHome(f); fileExists(f) {
				out = append(out, f)
			}
		}
		return out
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range []string{"known_hosts", "known_hosts2"} {
		p := filepath.Join(home, ".ssh", name)
		if fileExists(p) {
			out = append(out, p)
		}
	}
	return out
}

func (p *HostKeyPolicy) managedPath() string {
	if p.ManagedPath != "" {
		return p.ManagedPath
	}
	return defaultManagedKnownHosts()
}

func knownHostsLocations(e *knownhosts.KeyError) string {
	var b strings.Builder
	for _, k := range e.Want {
		if k.Filename != "" {
			fmt.Fprintf(&b, " (%s:%d)", k.Filename, k.Line)
		}
	}
	return b.String()
}

func asKeyError(err error, target **knownhosts.KeyError) bool {
	for err != nil {
		if ke, ok := err.(*knownhosts.KeyError); ok {
			*target = ke
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
