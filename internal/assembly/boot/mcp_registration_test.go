package boot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
)

func TestHostSessionMCPFailureIsVisible(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	host := plugin.NewHost()
	defer host.Close()
	registerHostSessionServer(context.Background(), host, tool.NewRegistry(), plugin.Spec{
		Name: "editor-tools", Type: "http", URL: server.URL, Authorized: true,
	}, nil)
	failures := host.Failures()
	if len(failures) != 1 || failures[0].Name != "editor-tools" || failures[0].HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("host session MCP failures = %+v", failures)
	}
}
