package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/state/sessionstore"
)

func TestInstallSourceReadRefusalReachesExecutorOutput(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (2<<20)+1)))
	}))
	t.Cleanup(srv.Close)
	reg := tool.NewRegistry()
	reg.Add(installsource.NewTool(installsource.Options{
		ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t), RequireApprovedPlan: true,
		HTTPClient: srv.Client(),
	}))
	a := New(nil, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		ID: "manifest-read", Name: "install_source", Arguments: fmt.Sprintf(`{"source":%q,"kind":"skill"}`, srv.URL+"/SKILL.md"),
	})
	if out.refusalCode != "install.source_unreadable" || !strings.Contains(out.output, "install.source_unreadable") {
		t.Fatalf("executor lost source identity: code=%q output=%q", out.refusalCode, out.output)
	}
}
