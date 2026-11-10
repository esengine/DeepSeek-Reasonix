package theme

import (
	"testing"

	"reasonix/internal/base/testenv"
)

func TestFoundationPalettesAreCompleteAndReadable(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	for _, id := range []string{"official-clear-blue", "official-tea-green"} {
		t.Run(id, func(t *testing.T) {
			pack, err := Load(id)
			if err != nil {
				t.Fatal(err)
			}
			if pack.Background != nil || pack.Sky != nil || len(pack.Warnings) != 0 {
				t.Fatalf("foundation must be plain and valid: %+v", pack)
			}
			for _, scheme := range []string{"light", "dark"} {
				tokens := pack.Tokens[scheme]
				for _, name := range []string{"radiusXs", "radiusSm", "radiusMd", "fontUi", "fontMono"} {
					if _, ok := tokens[name]; ok {
						t.Fatalf("palette changes %s", name)
					}
				}
				for _, name := range []string{"accentInk", "link", "brand", "labelAgent"} {
					for _, surface := range []string{"bg", "bgSoft", "bgElev", "panel", "float", "floatHi", "codeBg"} {
						if got := contrastRatio(t, tokens[name], tokens[surface]); got < 4.5 {
							t.Errorf("%s %s on %s: %.2f", scheme, name, surface, got)
						}
					}
				}
				if got := contrastRatio(t, tokens["accentFg"], tokens["accent"]); got < 4.5 {
					t.Errorf("%s button text contrast: %.2f", scheme, got)
				}
			}
		})
	}
}

func TestShippedPackInksReadOnEverySurfaceTheyAreSetOn(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	for _, p := range List() {
		for _, scheme := range []string{"light", "dark"} {
			tokens := p.Tokens[scheme]
			for _, ink := range []string{"fgDim", "fgFaint"} {
				if tokens[ink] == "" {
					continue
				}
				for _, surface := range []string{"bg", "bgSoft", "bgElev", "panel"} {
					if tokens[surface] == "" {
						continue
					}
					if got := contrastRatio(t, tokens[ink], tokens[surface]); got < 4.5 {
						t.Errorf("%s %s: %s on %s is %.2f, want at least 4.5", p.ID, scheme, ink, surface, got)
					}
				}
			}
		}
	}
}

// bgElev fills a selected navigation row and a segmented control's track. Its
// text contrast is held to AA elsewhere; this holds the fill apart from its
// ground: bg in light (navigation sits on the page), panel in dark (a control
// sits on a card).
func TestShippedPacksKeepTheirSelectionFillApartFromItsGround(t *testing.T) {
	pairs := map[string]struct {
		ground string
		floor  float64
	}{
		"light": {ground: "bg", floor: 1.02},
		"dark":  {ground: "panel", floor: 1.04},
	}
	for _, pack := range listBuiltin() {
		for scheme, want := range pairs {
			tokens := pack.Tokens[scheme]
			fill, ground := tokens["bgElev"], tokens[want.ground]
			if fill == "" || ground == "" {
				continue
			}
			if got := contrastRatio(t, fill, ground); got < want.floor {
				t.Errorf("%s %s: bgElev %s on %s %s is %.3f, below %.2f — a selected row or segmented control would vanish into its ground",
					pack.ID, scheme, fill, want.ground, ground, got, want.floor)
			}
		}
	}
}
