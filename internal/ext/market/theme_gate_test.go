package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/installsource"
)

const marketplaceSource = "https://github.com/acme/market/tree/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func themeMarketplace(t *testing.T, themes int, hook bool) string {
	t.Helper()
	root := testenv.TempDir(t)
	put := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var entries []map[string]string
	add := func(name, extra string) {
		entries = append(entries, map[string]string{"name": name, "source": "plugins/" + name})
		put("plugins/"+name+"/reasonix-plugin.json", `{"apiVersion":"reasonix.io/plugin/v2","name":"`+name+`","version":"1.0.0","contributes":{"themes":["themes/t.json"]}`+extra+`}`)
		put("plugins/"+name+"/themes/t.json", "{}")
	}
	for i := range themes {
		add(fmt.Sprintf("a%02d", i), "")
	}
	if hook {
		add("zz-hook", `,"hooks":{"SessionStart":[{"command":"echo hi"}]}`)
	}
	body, _ := json.Marshal(map[string]any{"name": "market", "plugins": entries})
	put(".claude-plugin/marketplace.json", string(body))
	return root
}

func themeService(t *testing.T, kind, root string) *Service {
	t.Helper()
	home := testenv.TempDir(t)
	newTool := func() *installsource.Tool {
		return installsource.NewTool(installsource.Options{
			ProjectRoot: testenv.TempDir(t), HomeDir: home, RequireApprovedPlan: true,
			PreparePlugin: func(context.Context, string, string) (string, string, func(), error) {
				return root, strings.Repeat("a", 40), func() {}, nil
			},
		})
	}
	raw, _ := newTool().Execute(t.Context(), json.RawMessage(`{"source":"`+marketplaceSource+`","kind":"plugin"}`))
	var plan struct {
		ContentDigest string `json:"contentDigest"`
	}
	_ = json.Unmarshal([]byte(raw), &plan)
	reg := &stubRegistry{detail: Detail{
		Package:  Package{Kind: kind, Slug: "acme/market", Status: "active", LatestVersion: "1.0.0"},
		Approved: &Version{Version: "1.0.0", Source: marketplaceSource, ContentHash: plan.ContentDigest},
	}}
	return &Service{Registry: reg, Home: filepath.Join(home, ".reasonix"), NewInstaller: newTool}
}

func TestThemeListingWithAHookPastTheDisplayCapIsRefused(t *testing.T) {
	root := themeMarketplace(t, 50, true)
	if _, err := themeService(t, "theme", root).Plan(t.Context(), Request{Slug: "acme/market"}); !errors.Is(err, ErrNotTheme) {
		t.Fatalf("a hook in the 51st package must keep it out of the theme category: %v", err)
	}
	out, err := themeService(t, "plugin", root).Plan(t.Context(), Request{Slug: "acme/market"})
	if err != nil {
		t.Fatal(err)
	}
	var actions []struct{ Name, RiskLevel string }
	_ = json.Unmarshal(out.Fields["actions"], &actions)
	var high bool
	for _, a := range actions {
		high = high || (a.Name == "zz-hook" && a.RiskLevel == "high")
	}
	if !high {
		t.Fatalf("the high-risk step must be in the envelope the person confirms: %s", out.Fields["actions"])
	}
}

func TestThemeListingPastTheDisplayCapIsNotRefused(t *testing.T) {
	root := themeMarketplace(t, 60, false)
	out, err := themeService(t, "theme", root).Plan(t.Context(), Request{Slug: "acme/market"})
	if err != nil {
		t.Fatalf("sixty pure themes are themes: %v", err)
	}
	var hidden int
	_ = json.Unmarshal(out.Fields["hiddenActions"], &hidden)
	if hidden != 10 {
		t.Fatalf("hiddenActions = %d", hidden)
	}
}
