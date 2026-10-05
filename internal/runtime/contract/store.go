package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/state/trustedstate"
)

// RecordKind names a contract revision's record in Trusted Host State.
const RecordKind = "contract/1"

// Seal appends c to stream and returns the record digest that names this
// revision from now on.
func Seal(ctx context.Context, store *trustedstate.Store, stream string, c Contract) (string, error) {
	_, d, err := store.Append(ctx, stream, RecordKind, c.Encode())
	return string(d), err
}

// Load reads the revision a record names. A record of another kind, or a
// payload that does not re-encode to the bytes it was read from, is tampered:
// a revision is its exact bytes.
func Load(store *trustedstate.Store, record string) (Contract, error) {
	rec, err := store.Record(trustedstate.Digest(record))
	if err != nil {
		return Contract{}, err
	}
	if rec.Kind != RecordKind {
		return Contract{}, fmt.Errorf("%w: record %s is %q, not a contract", trustedstate.ErrTampered, record, rec.Kind)
	}
	payload, err := store.Object(rec.Payload)
	if err != nil {
		return Contract{}, err
	}
	var c Contract
	if err := json.Unmarshal(payload, &c); err != nil || !bytes.Equal(c.Encode(), payload) {
		return Contract{}, fmt.Errorf("%w: contract %s does not re-encode to its bytes", trustedstate.ErrTampered, record)
	}
	return c, nil
}
