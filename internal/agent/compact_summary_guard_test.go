package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
)

func TestCompactionPrepareCannotExpandAutomaticSummaryPastWindow(t *testing.T) {
	const window = 60_000
	tests := []struct {
		name        string
		replacement func(*testing.T, json.RawMessage) protocol.InterceptResult
	}{
		{
			name: "messages",
			replacement: func(t *testing.T, _ json.RawMessage) protocol.InterceptResult {
				return replaceWith(t, dispatch.CompactionPreparePayload{
					Messages: []protocol.ProviderMessage{{
						Role: protocol.ProviderRoleUser, Content: strings.Repeat("x", window*4),
					}},
				})
			},
		},
		{
			name: "guidance",
			replacement: func(t *testing.T, raw json.RawMessage) protocol.InterceptResult {
				var payload dispatch.CompactionPreparePayload
				if err := json.Unmarshal(raw, &payload); err != nil {
					t.Fatalf("decode compaction.prepare payload: %v", err)
				}
				payload.Guidance = strings.Repeat("preserve expanded guidance ", window)
				return replaceWith(t, payload)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, raw json.RawMessage) (protocol.InterceptResult, error) {
				if ev == protocol.EventCompactionPrepare {
					return tc.replacement(t, raw), nil
				}
				return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
			}}
			prov := &opaqueWindowProvider{}
			a := agentOverForceWindow(t, prov, foldableSessionOverForce(120), window)
			a.svc.extensions = newExtDispatcher(client, true, nil, extension.PointCompactionPrepare)

			if err := prepareContext(context.Background(), a, CompactionTriggerPressure); !errors.Is(err, ErrCompactionRequired) {
				t.Fatalf("pressure maintenance error = %v, want fail-closed ErrCompactionRequired", err)
			}
			if len(prov.requests) != 0 {
				t.Fatalf("summary requests = %d, want none for an oversized extension replacement", len(prov.requests))
			}
			receipt := a.sess.compactionState.LastReceipt
			if receipt == nil || receipt.Status != "blocked" || !strings.Contains(receipt.Reason, "prepared summary request") {
				t.Fatalf("receipt = %+v, want the final summary-budget rejection", receipt)
			}
		})
	}
}
