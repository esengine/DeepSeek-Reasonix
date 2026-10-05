package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func writePack(t *testing.T, root, id, body string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const minimal = `{"schemaVersion":1,"id":"x","name":"Dusk","author":"me",
 "tokens":{"light":{"bg":"#FFFFFF","fg":"#111111"},"dark":{"bg":"#000000","fg":"#EEEEEE"}}}`

// The pack directory is the address a user activates by, so it is what the
// reader reports — a manifest claiming someone else's id cannot shadow them.
func TestLoadUsesTheDirectoryNameAsID(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	writePack(t, Dir(), "dusk", minimal)

	pack, err := Load("dusk")
	if err != nil {
		t.Fatal(err)
	}
	if pack.ID != "dusk" || pack.Name != "Dusk" {
		t.Fatalf("pack = %+v, want the directory id and manifest name", pack)
	}
	if pack.Tokens["dark"]["bg"] != "#000000" {
		t.Fatalf("dark tokens = %+v", pack.Tokens["dark"])
	}
}

// A token value is interpolated into a stylesheet by whoever renders it, so
// anything that is not a plain hex colour is dropped here rather than escaped
// downstream — a pack cannot smuggle a url(), an expression, or a brace.
func TestLoadKeepsOnlyPlainHexColours(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	writePack(t, Dir(), "sneaky", `{"schemaVersion":1,"name":"Sneaky","tokens":{
	  "light":{"bg":"#FFFFFF","evil":"url(http://x/y.png)","brace":"#fff}body{display:none","expr":"var(--x)"},
	  "dark":{"bg":"#000000"}}}`)

	pack, err := Load("sneaky")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pack.Tokens["light"]["bg"]; !ok {
		t.Fatal("the plain colour was dropped")
	}
	for _, key := range []string{"evil", "brace", "expr"} {
		if v, ok := pack.Tokens["light"][key]; ok {
			t.Fatalf("token %q survived as %q", key, v)
		}
	}
}

// A cosmetic file is not worth guessing at: a pack from a newer schema is
// skipped, and one bad pack must not hide the ones that are fine.
func TestListSkipsUnreadablePacksWithoutHidingTheRest(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	writePack(t, Dir(), "good", minimal)
	writePack(t, Dir(), "future", `{"schemaVersion":99,"name":"Future","tokens":{"light":{"bg":"#fff"},"dark":{"bg":"#000"}}}`)
	writePack(t, Dir(), "broken", `not json`)
	writePack(t, Dir(), "empty-dark", `{"schemaVersion":1,"name":"Half","tokens":{"light":{"bg":"#fff"}}}`)

	packs := List()
	installed := map[string]bool{}
	for _, p := range packs {
		installed[p.ID] = true
	}
	if !installed["good"] {
		t.Fatal("the readable pack was hidden by the broken ones")
	}
	for _, id := range []string{"future", "broken", "empty-dark"} {
		if installed[id] {
			t.Fatalf("unreadable pack %q was listed", id)
		}
	}
}

// With nothing installed the list is the shipped set, not an error and not
// empty: a fresh install has palettes to choose from.
func TestListWithNothingInstalledIsTheShippedSet(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	packs := List()
	if len(packs) == 0 {
		t.Fatal("List = empty, want the shipped packs")
	}
	for _, p := range packs {
		if !strings.HasPrefix(p.ID, "official-") {
			t.Fatalf("unexpected pack %q with nothing installed", p.ID)
		}
	}
}

func TestLoadRejectsAPathInsteadOfAnID(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	for _, id := range []string{"../escape", "a/b", ""} {
		if _, err := Load(id); err == nil {
			t.Fatalf("Load(%q) succeeded, want a rejection", id)
		}
	}
}

// The shipped packs are the starting point a fresh install has and the sample
// an agent copies when it authors one, so they have to decode with the same
// reader everything else goes through.
func TestShippedPacksAreReadable(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	packs := List()
	if len(packs) < 8 {
		t.Fatalf("List = %d packs, want the shipped set", len(packs))
	}
	for _, p := range packs {
		if p.Name == "" || len(p.Tokens["light"]) == 0 || len(p.Tokens["dark"]) == 0 {
			t.Fatalf("shipped pack %q is incomplete: %+v", p.ID, p)
		}
		// The frontend maps these two onto its page and text variables; a pack
		// without them would activate into an unreadable window.
		for _, key := range []string{"bg", "fg"} {
			if _, ok := p.Tokens["dark"][key]; !ok {
				t.Fatalf("shipped pack %q has no dark %q", p.ID, key)
			}
		}
	}
	if _, err := Load(packs[0].ID); err != nil {
		t.Fatalf("Load(%q): %v", packs[0].ID, err)
	}
}

// A user's own copy of a palette is the one they meant, so it shadows the
// shipped pack of the same id rather than appearing twice.
func TestInstalledPackShadowsTheShippedOne(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	shipped := List()
	if len(shipped) == 0 {
		t.Fatal("no shipped packs")
	}
	id := shipped[0].ID
	writePack(t, Dir(), id, `{"schemaVersion":1,"name":"Mine","tokens":{"light":{"bg":"#FFFFFF"},"dark":{"bg":"#000000"}}}`)

	after := List()
	if len(after) != len(shipped) {
		t.Fatalf("shadowing changed the count: %d -> %d", len(shipped), len(after))
	}
	for _, p := range after {
		if p.ID == id && p.Name != "Mine" {
			t.Fatalf("pack %q = %q, want the installed copy to win", id, p.Name)
		}
	}
	if pack, err := Load(id); err != nil || pack.Name != "Mine" {
		t.Fatalf("Load(%q) = %+v, %v", id, pack, err)
	}
}
