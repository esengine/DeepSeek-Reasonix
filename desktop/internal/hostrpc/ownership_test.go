package hostrpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSourceOwnershipPlatformStable(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tabs_windows.go", "tabs_linux.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package main\nimport ctx \"context\"\nfunc (*App) Read(c ctx.Context, tabID string, count int) {}\nfunc (*App) Current() {}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	owners, err := SourceOwnership(dir, "App")
	if err != nil {
		t.Fatal(err)
	}
	read := owners["Read"]
	if read.Domain != "desktop/tabs_linux.go|desktop/tabs_windows.go" || read.Owner != "App.Read" || read.Scope.Resolver != "App.Read" || read.Scope.Kind != "owner-inputs" || !reflect.DeepEqual(read.Scope.Inputs, []string{"tabID", "count"}) {
		t.Fatalf("Read = %+v", read)
	}
	if owners["Current"].Scope.Kind != "owner-state" {
		t.Fatal("no-input command must resolve owner state")
	}
	encoded, _ := json.Marshal(owners)
	if strings.Contains(string(encoded), dir) {
		t.Fatal("metadata must not contain absolute build paths")
	}
	if err := os.WriteFile(filepath.Join(dir, "tabs_windows.go"), []byte("package main\nfunc (*App) Read(projectID string, count int) {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SourceOwnership(dir, "App"); err == nil {
		t.Fatal("different platform input owners accepted")
	}
}

func TestRegistryOwnershipRequiredAndDigested(t *testing.T) {
	owners := map[string]CommandOwnership{"Void": {Domain: "fixture.go", Owner: "fixtureTarget.Void", Sources: []string{"fixture.go"}, Scope: CommandScope{Kind: "owner-state", Resolver: "fixtureTarget.Void", Inputs: []string{}}}}
	skip := Skip{}
	for _, cmd := range mustRegistry(t, &fixtureTarget{}, nil).Commands() {
		if cmd.Name != "Void" {
			skip[cmd.Name] = true
		}
	}
	r, err := NewRegistryWithOwners(&fixtureTarget{}, skip, owners)
	if err != nil {
		t.Fatal(err)
	}
	before := Build(r, nil).Digest()
	changed := owners["Void"]
	changed.Domain = "other.go"
	owners["Void"] = changed
	r, err = NewRegistryWithOwners(&fixtureTarget{}, skip, owners)
	if err != nil {
		t.Fatal(err)
	}
	if before == Build(r, nil).Digest() {
		t.Fatal("ownership excluded from digest")
	}
	if _, err := NewRegistryWithOwners(&fixtureTarget{}, nil, owners); err == nil {
		t.Fatal("missing metadata accepted")
	}
}
