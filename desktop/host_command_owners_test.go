package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"reasonix/desktop/internal/hostrpc"
)

func TestHostCommandOwnersMatchSource(t *testing.T) {
	want, err := hostrpc.SourceOwnership(".", "App")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]hostrpc.CommandOwnership
	if err := json.Unmarshal(hostCommandOwnersJSON, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("host command ownership drift; run go run . -emit-contract frontend/src/generated")
	}
	registry, err := newDesktopRegistry((*App)(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Commands()) != len(got) {
		t.Fatalf("source owners %d != reflected commands %d", len(got), len(registry.Commands()))
	}
	for _, cmd := range registry.Commands() {
		if cmd.Cancellation != "before-dispatch" {
			t.Errorf("%s unexpectedly promises cooperative cancellation", cmd.Name)
		}
	}
}
