package sandbox

import (
	"slices"
	"strings"
)

// HostAuthority names a host service a confined command may call. It is a
// semantic endpoint, never a path: the backends below resolve it, so policy
// never has to spell /var/run/docker.sock and Windows can one day satisfy the
// same contract without owning a Unix socket.
type HostAuthority string

const (
	// SSHAgent signs with keys the sandbox cannot read. Possession of the key
	// file and the authority to use it are different things, which is why
	// denying the read (ForbidReadRoots) leaves this wide open.
	SSHAgent HostAuthority = "ssh_agent"
	Docker   HostAuthority = "docker"
	Podman   HostAuthority = "podman"
)

// GovernedAuthorities is what this package can actually enforce. Naming the set
// is what keeps the claim honest: host IPC at large is not isolated — DBus,
// Wayland, gpg-agent, keyrings and arbitrary sockets remain reachable — these
// endpoints are governed.
func GovernedAuthorities() []HostAuthority { return []HostAuthority{SSHAgent, Docker, Podman} }

// ParseAuthorities maps configured names onto the vocabulary. Unknown names are
// dropped rather than failing the session: a config written for a newer
// Reasonix must not make bash unusable, and an unknown name grants nothing.
func ParseAuthorities(names []string) []HostAuthority {
	governed := map[HostAuthority]bool{}
	for _, a := range GovernedAuthorities() {
		governed[a] = true
	}
	out := make([]HostAuthority, 0, len(names))
	for _, name := range names {
		if a := HostAuthority(strings.TrimSpace(name)); governed[a] {
			out = append(out, a)
		}
	}
	return out
}

// Granted reports whether the spec allows calling this authority. A governed
// authority absent from the list is masked, so the zero Spec denies every one
// of them and a new call site is confined until it says otherwise.
func (s Spec) Granted(a HostAuthority) bool { return slices.Contains(s.HostAuthorities, a) }
