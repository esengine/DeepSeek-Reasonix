package theme

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A value lands inside a stylesheet, so the question is not "is this sensible"
// but "can this leave the property it was written into". Everything that could
// close a declaration, open a function, or fetch a URL has to be refused.
func TestTokenValuesCannotEscapeTheirDeclaration(t *testing.T) {
	escapes := []struct{ name, value string }{
		{"bg", "red; background-image: url(https://evil.example/x.png)"},
		{"bg", "#fff; }"},
		{"bg", "var(--err)"},
		{"radiusSm", "8px; position: fixed"},
		{"radiusSm", "calc(100% - 2px)"},
		{"radiusSm", "var(--r-md)"},
		{"fontUi", `Arial; } body { display: none`},
		{"fontUi", `local("x"), url(https://evil.example/f.woff2)`},
		{"fontUi", "Arial\\3b color:red"},
	}
	for _, esc := range escapes {
		if validToken(esc.name, esc.value) {
			t.Errorf("validToken(%q, %q) accepted a value that can leave its declaration", esc.name, esc.value)
		}
	}
}

func TestTokenValuesAcceptWhatAPackActuallyWrites(t *testing.T) {
	ok := []struct{ name, value string }{
		{"bg", "#0F0D0B"},
		{"accent", "#d89b5a"},
		{"accentFg", "#fff"},
		{"bgSoft", "#11223344"},
		{"radiusXs", "3px"},
		{"radiusMd", "0.5rem"},
		{"radiusSm", "0"},
		{"fontUi", `-apple-system, "Segoe UI", sans-serif`},
		{"fontMono", `ui-monospace, "SF Mono", Menlo, monospace`},
		{"fontUi", "苹方, PingFang SC, sans-serif"},
	}
	for _, entry := range ok {
		if !validToken(entry.name, entry.value) {
			t.Errorf("validToken(%q, %q) refused a value a pack would reasonably write", entry.name, entry.value)
		}
	}
}

// A radius is a size, and a size has a ceiling: past it the value is not a
// rounder card, it is a different shape.
func TestLengthHasACeiling(t *testing.T) {
	if validToken("radiusMd", "999px") {
		t.Error("a 999px radius was accepted; that is a pill, not a rounded corner")
	}
	if !validToken("radiusMd", "64px") {
		t.Error("the documented ceiling was refused")
	}
}

// A name outside the vocabulary is refused whatever its value looks like: that
// is what stops a pack from reaching a variable the frontend never meant to
// hand over, including the ones that carry meaning.
func TestUnknownAndReservedNamesAreRefused(t *testing.T) {
	for _, name := range []string{"ok", "err", "radiusPill", "bgColor", ""} {
		if validToken(name, "#ffffff") {
			t.Errorf("validToken accepted %q, which is not in the vocabulary", name)
		}
	}
}

// The packs that ship are held to the vocabulary they document. Introducing
// this check found four dead tokens in every one of them — chat/sidebar/
// workspace/workspaceFiles, named for regions of an editor this frontend does
// not have — which had been silently dropped since the packs were ported.
func TestShippedPacksUseOnlyRealTokens(t *testing.T) {
	packs := listBuiltin()
	if len(packs) == 0 {
		t.Fatal("no packs ship; the embed is broken")
	}
	for _, pack := range packs {
		if len(pack.Warnings) > 0 {
			t.Errorf("%s: %v", pack.ID, pack.Warnings)
		}
	}
}

// Bold text in the transcript is the one ink a reader asks to tell apart by
// colour rather than weight, so a pack can name it without arbitrary CSS.
func TestDecodeAcceptsAStrongTextColour(t *testing.T) {
	pack, err := decode([]byte(`{
      "schemaVersion": 1,
      "name": "Strong",
      "tokens": {
        "light": {"fg": "#1a1a1a", "fgStrong": "#8a3b12"},
        "dark":  {"fg": "#f0f0f0", "fgStrong": "#f2b27a"}
      }
    }`), "strong")
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Warnings) > 0 {
		t.Fatalf("warnings = %v, want none", pack.Warnings)
	}
	if got := pack.Tokens["dark"]["fgStrong"]; got != "#f2b27a" {
		t.Fatalf("fgStrong = %q, want it kept", got)
	}
}

// The pack still loads when one value is wrong, and says which one. Dropping
// the whole pack would cost the author every good token for one typo; dropping
// the token silently would leave them with no way to find it.
func TestDecodeKeepsGoodTokensAndReportsBadOnes(t *testing.T) {
	pack, err := decode([]byte(`{
      "schemaVersion": 1,
      "name": "Partial",
      "tokens": {
        "light": {"bg": "#ffffff", "fg": "#000000", "radiusSm": "huge", "glow": "#ff0000"},
        "dark":  {"bg": "#000000", "fg": "#ffffff"}
      }
    }`), "partial")
	if err != nil {
		t.Fatal(err)
	}
	if got := pack.Tokens["light"]["bg"]; got != "#ffffff" {
		t.Fatalf("good token was lost: %q", got)
	}
	if _, present := pack.Tokens["light"]["radiusSm"]; present {
		t.Error("an invalid length survived into the pack")
	}
	if len(pack.Warnings) != 2 {
		t.Fatalf("warnings = %v, want one for the bad value and one for the unknown name", pack.Warnings)
	}
}

// Contrast is a property of a pair, so it is the one thing the token validator
// cannot see: every colour in a pack is a legal value on its own. What we ship
// is held to AA on the surfaces its text lands on, because the default
// appearance already is — below that line a pack does not read as themed, it
// reads as a window that has gone wrong.
func TestShippedPacksMeetTextContrast(t *testing.T) {
	const aa = 4.5
	inks := []string{"fg", "fgStrong", "fgDim", "fgFaint"}
	surfaces := []string{"bg", "bgSoft", "panel", "bgElev", "float", "floatHi", "codeBg", "sunkBg"}
	packs := listBuiltin()
	if len(packs) == 0 {
		t.Fatal("no packs ship; the embed is broken")
	}
	for _, pack := range packs {
		for _, scheme := range []string{"light", "dark"} {
			tokens := pack.Tokens[scheme]
			for _, ink := range inks {
				for _, surface := range surfaces {
					fg, ok := tokens[ink]
					bg, ok2 := tokens[surface]
					if !ok || !ok2 {
						continue
					}
					if got := contrastRatio(t, fg, bg); got < aa {
						t.Errorf("%s %s: %s %s on %s %s is %.2f, below AA %.2f",
							pack.ID, scheme, ink, fg, surface, bg, got, aa)
					}
				}
			}
		}
	}
}

func contrastRatio(t *testing.T, a, b string) float64 {
	t.Helper()
	la, lb := relativeLuminance(t, a), relativeLuminance(t, b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relativeLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimPrefix(hex, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("%q is not a six-digit hex colour: %v", hex, err)
	}
	lin := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// Code is read on its own ground, so each syntax colour a pack ships is held to
// the same AA line on the code background that text is held to elsewhere.
func TestShippedPacksMeetSyntaxContrast(t *testing.T) {
	const aa = 4.5
	for _, pack := range listBuiltin() {
		for _, scheme := range []string{"light", "dark"} {
			tokens := pack.Tokens[scheme]
			bg, ok := tokens["codeBg"]
			if !ok {
				t.Errorf("%s %s: no codeBg", pack.ID, scheme)
				continue
			}
			for _, name := range []string{"synKeyword", "synString", "synNumber", "synFunction"} {
				fg, ok := tokens[name]
				if !ok {
					t.Errorf("%s %s: no %s", pack.ID, scheme, name)
					continue
				}
				if got := contrastRatio(t, fg, bg); got < aa {
					t.Errorf("%s %s: %s %s on codeBg %s is %.2f, below AA", pack.ID, scheme, name, fg, bg, got)
				}
			}
		}
	}
}

// The vocabulary is the kernel's and the mapping onto CSS variables is the
// frontend's; a token one side knows and the other does not is either accepted
// and never painted, or painted and never accepted.
func TestThemeTokenVocabularyMatchesTheFrontend(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "desktop", "frontend-next", "src", "ui", "theme.ts"))
	if err != nil {
		t.Skipf("frontend source not present: %v", err)
	}
	block := regexp.MustCompile(`(?s)const SURFACE: Record<string, string\[\]> = \{(.*?)\n\};`).FindSubmatch(src)
	if block == nil {
		t.Fatal("theme.ts no longer declares SURFACE; update this test with it")
	}
	var frontend []string
	for _, m := range regexp.MustCompile(`(?m)^\s*([A-Za-z]+):\s*\[`).FindAllSubmatch(block[1], -1) {
		frontend = append(frontend, string(m[1]))
	}
	slices.Sort(frontend)
	if kernel := TokenNames(); !slices.Equal(frontend, kernel) {
		t.Fatalf("frontend maps %v, kernel accepts %v", frontend, kernel)
	}
}
