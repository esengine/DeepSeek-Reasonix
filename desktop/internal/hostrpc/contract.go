package hostrpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"slices"
)

// ProtocolVersion is the wire revision both sides must agree on in hello.
const ProtocolVersion = 1

// Contract is everything the shell needs to call the service: the accepted
// commands, the event names it may receive and every DTO shape they use.
type Contract struct {
	ProtocolVersion int                   `json:"protocolVersion"`
	Commands        []Command             `json:"commands"`
	Events          []string              `json:"events"`
	Types           map[string]ObjectType `json:"types"`
}

// Build freezes a registry plus the event names into a Contract. Events are
// sorted and deduplicated so the digest never depends on caller order.
func Build(r *Registry, events []string) Contract {
	sorted := slices.Compact(slices.Sorted(slices.Values(events)))
	if sorted == nil {
		sorted = []string{}
	}
	types := maps.Clone(r.types)
	if types == nil {
		types = map[string]ObjectType{}
	}
	return Contract{
		ProtocolVersion: ProtocolVersion,
		Commands:        r.Commands(),
		Events:          sorted,
		Types:           types,
	}
}

// Canonical is the contract as sorted-key JSON without whitespace: the bytes
// the digest covers and the shell must reproduce bit for bit.
func (c Contract) Canonical() []byte {
	raw, err := json.Marshal(c)
	if err != nil {
		panic("hostrpc: contract is not JSON-serialisable: " + err.Error())
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		panic("hostrpc: contract JSON does not round-trip: " + err.Error())
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		panic("hostrpc: canonical contract encode: " + err.Error())
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// Digest is "sha256:" plus the hex SHA-256 of Canonical.
func (c Contract) Digest() string {
	sum := sha256.Sum256(c.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// WriteJSON writes the canonical contract indented for review, ending in a
// newline. Key order matches Canonical so the two never disagree.
func WriteJSON(w io.Writer, c Contract) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, c.Canonical(), "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}
