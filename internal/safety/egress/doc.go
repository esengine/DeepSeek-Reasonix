// Package egress is the host-side proxy a sandboxed command reaches the network
// through when its egress is limited to named domains. The OS sandbox leaves
// the command one way out, this listener on loopback, and the proxy decides per
// connection from the host the client asked for. A refusal is answered with a
// typed reason the client sees and the host records against the command that
// caused it, so a failed download can be attributed to policy instead of left
// for the model to guess at.
//
// The decision reads the requested name, never the TLS stream: a client that
// fronts a disallowed host behind an allowed one is not caught, and allowing a
// broad host such as github.com leaves everything that host serves reachable.
package egress
