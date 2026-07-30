package remote

import (
	"bufio"
	"bytes"
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

// KnownHostLocation identifies the OpenSSH record that conflicts with a
// presented host key. It is intentionally structured so desktop clients can
// keep machine-local paths out of the primary error message while still
// exposing the exact record in an explicit security-details view.
type KnownHostLocation struct {
	Filename string
	Line     int
}

// HostKeyMismatchError describes a presented key that contradicts an existing
// known_hosts record. It unwraps to ErrHostKeyMismatch so callers can retain
// the existing fail-closed classification without parsing error strings.
type HostKeyMismatchError struct {
	Host                 string
	PresentedFingerprint string
	Locations            []KnownHostLocation
}

func (e *HostKeyMismatchError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s for %s: presented %s; known_hosts records a different key",
		ErrHostKeyMismatch, e.Host, e.PresentedFingerprint)
	for _, location := range e.Locations {
		if location.Filename != "" {
			fmt.Fprintf(&b, " (%s:%d)", location.Filename, location.Line)
		}
	}
	return b.String()
}

func (e *HostKeyMismatchError) Unwrap() error { return ErrHostKeyMismatch }

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
	// Verified observes a key only after the known_hosts check (and, for TOFU,
	// the user's acceptance and durable append) succeeded. It lets an assembly
	// layer bind higher-level capabilities to the peer actually authenticated by
	// this transport without weakening HostKeyCallback authority.
	Verified func(HostKeyQuestion)

	mu sync.Mutex // serializes appends to ManagedPath
}

// Callback builds an ssh.HostKeyCallback enforcing this policy for host (the
// display label used in prompts). ctx bounds any interactive prompt.
func (p *HostKeyPolicy) Callback(ctx context.Context, host string) (ssh.HostKeyCallback, error) {
	base, managed, err := p.loadCallback()
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if base != nil {
			err := base(hostname, remote, key)
			if err == nil {
				p.notifyVerified(host, hostname, remote, key)
				return nil
			}
			var keyErr *knownhosts.KeyError
			if !asKeyError(err, &keyErr) {
				return err
			}
			if len(keyErr.Want) > 0 {
				// A different key is on record for this host: hard fail, never
				// promptable. Name the file:line so the user can inspect it.
				return newHostKeyMismatchError(host, ssh.FingerprintSHA256(key), keyErr)
			}
			// len(Want)==0 => host unknown. Fall through to TOFU.
		}
		if err := p.tofu(ctx, host, hostname, remote, key, managed); err != nil {
			return err
		}
		p.notifyVerified(host, hostname, remote, key)
		return nil
	}, nil
}

func (p *HostKeyPolicy) notifyVerified(host, hostname string, remoteAddr net.Addr, key ssh.PublicKey) {
	if p == nil || p.Verified == nil || key == nil {
		return
	}
	address := hostname
	if remoteAddr != nil && strings.TrimSpace(remoteAddr.String()) != "" {
		address = remoteAddr.String()
	}
	p.Verified(HostKeyQuestion{
		Host: host, Address: address, KeyType: key.Type(), Fingerprint: ssh.FingerprintSHA256(key),
	})
}

// HostKeyAlgorithms returns host-key algorithms in negotiation order,
// preferring algorithms compatible with ordinary host identities already
// recorded for hostname. Certificate-authority records are deliberately not
// treated as host keys: the CA algorithm does not describe the certified host
// key. The strict callback remains the authority for every negotiated key.
func (p *HostKeyPolicy) HostKeyAlgorithms(hostname string, remote net.Addr) ([]string, error) {
	base, _, err := p.loadCallback()
	if err != nil || base == nil {
		return nil, err
	}
	err = base(hostname, remote, hostKeyLookupProbe{})
	if err == nil {
		return nil, nil
	}
	var keyErr *knownhosts.KeyError
	if !asKeyError(err, &keyErr) {
		return nil, err
	}
	if len(keyErr.Want) == 0 {
		return nil, nil
	}

	preferred := make(map[string]bool, len(keyErr.Want))
	for _, known := range keyErr.Want {
		if known.Key == nil {
			continue
		}
		marker, err := knownHostMarker(known)
		if err != nil {
			return nil, err
		}
		if marker != "" {
			continue
		}
		keyType := known.Key.Type()
		preferred[keyType] = true
		switch keyType {
		case ssh.KeyAlgoRSA:
			// An ssh-rsa public key can use the SHA-2 signature algorithms;
			preferred[ssh.KeyAlgoRSASHA512] = true
			preferred[ssh.KeyAlgoRSASHA256] = true
		case ssh.CertAlgoRSAv01:
			// RSA host certificates likewise support SHA-2 signature
			// algorithms even though their public key format is ssh-rsa.
			preferred[ssh.CertAlgoRSASHA512v01] = true
			preferred[ssh.CertAlgoRSASHA256v01] = true
		}
	}

	candidates := hostKeyAlgorithmCandidates()
	ordered := make([]string, 0, len(candidates))
	for _, algorithm := range candidates {
		if preferred[algorithm] {
			ordered = append(ordered, algorithm)
		}
	}
	if len(ordered) == 0 {
		return nil, nil
	}
	for _, algorithm := range candidates {
		if !preferred[algorithm] {
			ordered = append(ordered, algorithm)
		}
	}
	return ordered, nil
}

// hostKeyAlgorithmCandidates preserves the algorithms in the Go SSH default
// policy while keeping secure algorithms ahead of legacy fallbacks. Legacy
// algorithms are only promoted when their exact public key format is already
// recorded; the host-key callback must still verify the key material.
func hostKeyAlgorithmCandidates() []string {
	secure := ssh.SupportedAlgorithms().HostKeys
	legacy := ssh.InsecureAlgorithms().HostKeys
	algorithms := make([]string, 0, len(secure)+len(legacy))
	seen := make(map[string]bool, cap(algorithms))
	for _, algorithm := range append(secure, legacy...) {
		if !seen[algorithm] {
			seen[algorithm] = true
			algorithms = append(algorithms, algorithm)
		}
	}
	return algorithms
}

// knownHostMarker reads the original matching record so @cert-authority and
// @revoked entries cannot be mistaken for ordinary host identities. KnownKey
// exposes the exact file and line selected by knownhosts.New; ParseKnownHosts
// supplies OpenSSH marker semantics without duplicating its parser.
func knownHostMarker(known knownhosts.KnownKey) (string, error) {
	if known.Filename == "" || known.Line <= 0 {
		return "", fmt.Errorf("known_hosts record has no source location")
	}
	f, err := os.Open(known.Filename)
	if err != nil {
		return "", fmt.Errorf("open known_hosts record %s:%d: %w", known.Filename, known.Line, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		if line != known.Line {
			continue
		}
		marker, _, key, _, _, err := ssh.ParseKnownHosts(scanner.Bytes())
		if err != nil {
			return "", fmt.Errorf("parse known_hosts record %s:%d: %w", known.Filename, known.Line, err)
		}
		if key == nil || known.Key == nil || !bytes.Equal(key.Marshal(), known.Key.Marshal()) {
			return "", fmt.Errorf("known_hosts record changed while connecting: %s:%d", known.Filename, known.Line)
		}
		return marker, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read known_hosts record %s:%d: %w", known.Filename, known.Line, err)
	}
	return "", fmt.Errorf("known_hosts record no longer exists: %s:%d", known.Filename, known.Line)
}

// hostKeyLookupProbe deliberately cannot equal a parsed OpenSSH public key.
// Passing it through knownhosts.New lets us reuse the library's exact hostname,
// wildcard, hashed-host, port, and file matching and inspect KeyError.Want.
type hostKeyLookupProbe struct{}

func (hostKeyLookupProbe) Type() string    { return "reasonix-host-key-lookup-probe" }
func (hostKeyLookupProbe) Marshal() []byte { return []byte("reasonix-host-key-lookup-probe") }
func (hostKeyLookupProbe) Verify([]byte, *ssh.Signature) error {
	return fmt.Errorf("host-key lookup probe cannot verify signatures")
}

func (p *HostKeyPolicy) loadCallback() (ssh.HostKeyCallback, string, error) {
	files := p.systemFiles()
	managed := p.managedPath()
	if managed != "" {
		if err := os.MkdirAll(filepath.Dir(managed), 0o700); err != nil {
			return nil, "", err
		}
		// knownhosts.New requires each file to exist; create an empty managed
		// file on first use.
		if _, err := os.Stat(managed); os.IsNotExist(err) {
			if err := os.WriteFile(managed, nil, 0o600); err != nil {
				return nil, "", err
			}
		}
		files = append(files, managed)
	}

	var base ssh.HostKeyCallback
	if len(files) > 0 {
		var err error
		base, err = knownhosts.New(files...)
		if err != nil {
			return nil, "", fmt.Errorf("load known_hosts: %w", err)
		}
	}
	return base, managed, nil
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

func newHostKeyMismatchError(host, presented string, e *knownhosts.KeyError) error {
	locations := make([]KnownHostLocation, 0, len(e.Want))
	for _, k := range e.Want {
		locations = append(locations, KnownHostLocation{Filename: k.Filename, Line: k.Line})
	}
	return &HostKeyMismatchError{Host: host, PresentedFingerprint: presented, Locations: locations}
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
