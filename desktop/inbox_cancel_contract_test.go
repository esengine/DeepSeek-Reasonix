package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInboxCancelResultViewMarshalsEmptyIDsAsArray(t *testing.T) {
	view := InboxCancelResultView{DiscardedItemIDs: []string{}}
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, `"discardedItemIds":[]`) {
		t.Fatalf("cancel result JSON = %s, want non-null array", got)
	}
}
