package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContextMaintenanceInfoUsesOptionalCamelCaseWailsFields(t *testing.T) {
	b, err := json.Marshal(ContextInfo{Maintenance: &ContextMaintenanceInfo{
		ProjectedTokens: 1200,
		LastReceipt: &ContextMaintenanceReceiptInfo{
			OperationID: "op", Action: "prune", SavedTokens: 4096,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"maintenance"`, `"projectedTokens":1200`, `"lastReceipt"`, `"operationId":"op"`, `"savedTokens":4096`} {
		if !strings.Contains(got, want) {
			t.Fatalf("ContextInfo JSON missing %s: %s", want, got)
		}
	}
	if strings.Contains(got, "operation_id") || strings.Contains(got, "saved_tokens") {
		t.Fatalf("ContextInfo leaked sidecar snake_case into Wails JSON: %s", got)
	}
}
