package typesafe

import (
	"context"
	"encoding/json"
	"fmt"
)

// Probe is how this protocol is verified: one System One round trip with the
// smallest valid question. The service has no model listing, so reaching its
// decision endpoint and getting a well-formed verdict back is the whole proof.
func (c Client) Probe(ctx context.Context, model string) error {
	result, err := c.Evaluate(ctx, Request{
		State: "Connectivity probe",
		Model: model,
		Questions: map[string]Question{
			"probe": {Type: "noul", Instructions: "Is this a connectivity probe?"},
		},
	})
	if err != nil {
		return err
	}
	var answer struct {
		Type string   `json:"type"`
		Noul *float64 `json:"noul"`
	}
	if json.Unmarshal(result.Answers["probe"], &answer) != nil || answer.Type != "noul" || answer.Noul == nil || *answer.Noul < 0 || *answer.Noul > 1 {
		return fmt.Errorf("%w: no valid Noul answer", ErrMalformedResponse)
	}
	return nil
}
