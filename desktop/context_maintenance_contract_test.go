package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContextMaintenanceInfoUsesOptionalCamelCaseWailsFields(t *testing.T) {
	b, err := json.Marshal(ContextInfo{Maintenance: &ContextMaintenanceInfo{
		CanonicalTokens: 800_000, ProjectedTokens: 160_000,
		TriggerTokens: 850_000, CheckpointState: "applied",
		LastReceipt: &ContextMaintenanceReceiptInfo{
			OperationID: "op", Action: "summary", SavedTokens: 4096,
		},
		ContextBudget: &ContextBudgetInfo{WindowMode: "shared", Source: "official", WindowTokens: 1_048_576},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"maintenance"`, `"projectedTokens":160000`, `"canonicalTokens":800000`,
		`"triggerTokens":850000`, `"checkpointState":"applied"`,
		`"lastReceipt"`, `"operationId":"op"`, `"savedTokens":4096`, `"action":"summary"`,
		`"contextBudget"`, `"windowMode":"shared"`, `"source":"official"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ContextInfo JSON missing %s: %s", want, got)
		}
	}
	if strings.Contains(got, "operation_id") || strings.Contains(got, "saved_tokens") {
		t.Fatalf("ContextInfo leaked sidecar snake_case into Wails JSON: %s", got)
	}
}
