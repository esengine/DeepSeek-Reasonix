// capability_errors.go — what fails when a capability switch is asked about,
// as identities rather than sentences. A name nobody declared is the user's to
// correct; a store that would not answer is not, and a caller that cannot tell
// them apart reports the second as the first.
package config

import (
	"errors"
	"fmt"
)

// ServerNotFoundError is the answer to a name no configured MCP server
// answers to. It carries the name because the sentence a frontend renders is
// built from it, not parsed out of this one.
type ServerNotFoundError struct{ Name string }

func (e *ServerNotFoundError) Error() string {
	return fmt.Sprintf("no configured MCP server named %q", e.Name)
}

// ErrActivationUnavailable is the durable switch store failing to answer — read,
// write or lock — because those are one answer to whoever flipped the switch: it
// did not stick, and none of them is their doing. A file that is present but
// malformed is UnparsedFile instead, which names something they can go and fix.
var ErrActivationUnavailable = errors.New("the activation store could not be reached")
