package browseripc

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// schemaJSON is the canonical wire protocol document. Every Go type and
// constant in this package mirrors it; TestSchemaParity proves they cannot
// drift. The TypeScript companion types are generated from the same bytes by
// cmd/browser-ipc-gen.
//
//go:embed schema.json
var schemaJSON []byte

// SchemaJSON returns the canonical schema document bytes.
func SchemaJSON() []byte { return schemaJSON }

// SchemaHash returns "sha256:<hex>" over the exact schema bytes, used to pin
// the generated TypeScript artifact to the same document.
func SchemaHash() string {
	sum := sha256.Sum256(schemaJSON)
	return "sha256:" + hex.EncodeToString(sum[:])
}
