// Package forward manages SSH port-forward rules bound to a live connection.
// Local (-L) forwards keep their local listener open across reconnects so a
// forwarded serve URL survives an outage; remote (-R) forwards are
// re-registered on every re-attach because they die with the SSH connection.
package forward

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Direction is the forward direction.
type Direction int

const (
	// Local is an -L forward: listen locally, dial from the remote side.
	Local Direction = iota
	// Remote is an -R forward: listen on the remote side, dial locally.
	Remote
)

func (d Direction) String() string {
	if d == Remote {
		return "remote"
	}
	return "local"
}

// Spec describes one forward rule.
type Spec struct {
	Name       string // stable id; derived from bind/target when empty
	Direction  Direction
	BindAddr   string // "127.0.0.1:8080"; ":0" allowed for Local (ephemeral)
	TargetAddr string // "127.0.0.1:80"
}

// Validate rejects malformed bind/target addresses before a listener is
// registered. Target port zero is never dialable; bind port zero remains
// valid for an ephemeral local/remote listener.
func (s Spec) Validate() error {
	if s.Direction != Local && s.Direction != Remote {
		return fmt.Errorf("forward: invalid direction %d", s.Direction)
	}
	if err := validateAddress(s.BindAddr, true); err != nil {
		return fmt.Errorf("forward: invalid bind address %q: %w", s.BindAddr, err)
	}
	if err := validateAddress(s.TargetAddr, false); err != nil {
		return fmt.Errorf("forward: invalid target address %q: %w", s.TargetAddr, err)
	}
	return nil
}

func validateAddress(addr string, allowZero bool) error {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return err
	}
	if host == "" && !allowZero {
		return errors.New("host is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 || (!allowZero && port == 0) {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

// Typed errors.
var (
	ErrBindBusy         = errors.New("forward: bind address in use")
	ErrDuplicateForward = errors.New("forward: duplicate name")
	ErrNotAttached      = errors.New("forward: not attached to a connection")
)

// DefaultName derives a stable name for a spec that did not set one.
func (s Spec) DefaultName() string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("%s:%s->%s", dirShort(s.Direction), s.BindAddr, s.TargetAddr)
}

func dirShort(d Direction) string {
	if d == Remote {
		return "R"
	}
	return "L"
}

// ParseDirection maps "local"/"-L"/"L" and "remote"/"-R"/"R".
func ParseDirection(s string) (Direction, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "local", "-l", "l":
		return Local, nil
	case "remote", "-r", "r":
		return Remote, nil
	default:
		return Local, fmt.Errorf("forward: direction must be local (-L) or remote (-R), got %q", s)
	}
}

// ParseShorthand parses OpenSSH-style forward shorthands:
//
//	"8080:host:80"          -> bind 127.0.0.1:8080, target host:80
//	"127.0.0.1:8080:host:80"-> bind 127.0.0.1:8080, target host:80
//	"8080"                  -> bind 127.0.0.1:8080, target 127.0.0.1:8080
//
// Bare ports and unqualified binds default to loopback (127.0.0.1).
func ParseShorthand(dir Direction, s string) (Spec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Spec{}, fmt.Errorf("forward: empty spec")
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		p, err := parseTargetPort(parts[0])
		if err != nil {
			return Spec{}, err
		}
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(p))
		return Spec{Direction: dir, BindAddr: addr, TargetAddr: addr}, nil
	case 3:
		// bindPort:targetHost:targetPort
		bp, err := parsePort(parts[0])
		if err != nil {
			return Spec{}, err
		}
		tp, err := parseTargetPort(parts[2])
		if err != nil {
			return Spec{}, err
		}
		if parts[1] == "" {
			return Spec{}, fmt.Errorf("forward: empty target host in %q", s)
		}
		return Spec{
			Direction:  dir,
			BindAddr:   net.JoinHostPort("127.0.0.1", strconv.Itoa(bp)),
			TargetAddr: net.JoinHostPort(parts[1], strconv.Itoa(tp)),
		}, nil
	case 4:
		// bindHost:bindPort:targetHost:targetPort
		bp, err := parsePort(parts[1])
		if err != nil {
			return Spec{}, err
		}
		tp, err := parseTargetPort(parts[3])
		if err != nil {
			return Spec{}, err
		}
		if parts[2] == "" {
			return Spec{}, fmt.Errorf("forward: empty target host in %q", s)
		}
		bindHost := parts[0]
		if bindHost == "" {
			bindHost = "127.0.0.1"
		}
		return Spec{
			Direction:  dir,
			BindAddr:   net.JoinHostPort(bindHost, strconv.Itoa(bp)),
			TargetAddr: net.JoinHostPort(parts[2], strconv.Itoa(tp)),
		}, nil
	default:
		return Spec{}, fmt.Errorf("forward: cannot parse spec %q", s)
	}
}

// NonLoopbackBind reports whether the spec binds a non-loopback address, which
// callers surface as a warning (the forward becomes reachable off-machine).
func (s Spec) NonLoopbackBind() bool {
	host, _, err := net.SplitHostPort(s.BindAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	switch {
	case host == "" || host == "*":
		return true
	case ip == nil:
		return host != "localhost"
	default:
		return !ip.IsLoopback()
	}
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p < 0 || p > 65535 {
		return 0, fmt.Errorf("forward: invalid port %q", s)
	}
	return p, nil
}

func parseTargetPort(s string) (int, error) {
	p, err := parsePort(s)
	if err != nil || p == 0 {
		return 0, fmt.Errorf("forward: invalid target port %q", s)
	}
	return p, nil
}
