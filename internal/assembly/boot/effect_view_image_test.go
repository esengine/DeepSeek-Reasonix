package boot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// view_image is offered to a model that reads images and to no other: a session
// on a text-only model keeps exactly the tool block it had, and so its cached
// prompt prefix. What reaches the provider is checked, not the registry.
func TestEffectViewImageReachesOnlyAModelThatReadsImages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		vision      bool
		composition string
		enabled     string
		missing     string
	}{
		{true, "", "", ""}, {false, "", "", ""},
		{true, "builtin:general", "", ""}, {false, "builtin:general", "", ""},
		{true, "builtin:minimal", "", ""}, {false, "builtin:minimal", "", ""},
		{true, "builtin:minimal", `["bash"]`, ""}, {false, "builtin:minimal", `["bash"]`, ""},
		{true, "builtin:minimal", `["view_image"]`, "tools:bash"},
	} {
		t.Run(fmt.Sprintf("vision=%v/composition=%s/enabled=%s", tc.vision, tc.composition, tc.enabled), func(t *testing.T) {
			t.Parallel()
			home := statedBootHome(t)
			search := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("tool-schema inspection unexpectedly called web search")
			}))
			t.Cleanup(search.Close)
			writeFile(t, home, "config.toml", fmt.Sprintf("[tools]\nweb_search_backends = [\"searxng\"]\nsearxng_url = %q\n", search.URL))
			if tc.enabled != "" {
				writeFile(t, home, "config.toml", fmt.Sprintf("[tools]\nenabled = %s\nweb_search_backends = [\"searxng\"]\nsearxng_url = %q\n", tc.enabled, search.URL))
			}
			dir := testenv.TempDir(t)
			rec := &peerToolProvider{}
			kind := fmt.Sprintf("boot-view-image-%s", strings.ReplaceAll(t.Name(), "/", "-"))
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = %q
model = "x"
vision = %v
`, kind, tc.vision))
			approveWorkspaceAt(t, home, dir)
			ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard, AgentComposition: tc.composition})
			if tc.missing != "" {
				var diagnostic *agentcomposition.DiagnosticError
				if !errors.As(err, &diagnostic) || len(diagnostic.Diagnostics) != 1 || diagnostic.Diagnostics[0].Module != tc.missing {
					t.Fatalf("error = %v, want missing required module %s", err, tc.missing)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = ctrl.Run(ctx, "hi")
			if !slices.Contains(rec.toolNames(), "bash") {
				t.Fatalf("required bash tool missing from provider request: %v", rec.toolNames())
			}
			has := false
			for _, name := range rec.toolNames() {
				has = has || name == "view_image"
			}
			want := tc.vision && tc.enabled == ""
			if has != want {
				t.Fatalf("vision=%v composition=%s: tools in the first request = %v", tc.vision, tc.composition, rec.toolNames())
			}
		})
	}
}
