package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func trustServer(t *testing.T, root string) (*httptest.Server, *control.Controller, http.Header) {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, WorkspaceRoot: root, Posture: control.PostureEvidence{WritesConfined: true, Home: testenv.TempDir(t)}})
	ctrl.ApplyDefaultPosture()
	s := New(ctrl, bc, config.ServeConfig{AuthMode: "none"})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, ctrl, http.Header{"Authorization": {"Bearer " + s.AuthToken()}}
}

// The answer lands in the kernel, moves a defaulted session, and comes back as
// the posture report the page redraws from.
func TestWorkspaceTrustMovesADefaultedSession(t *testing.T) {
	srv, ctrl, auth := trustServer(t, testenv.TempDir(t))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/workspace-trust", strings.NewReader(`{"trust":"trusted"}`))
	req.Header = auth.Clone()
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got control.PostureReport
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /workspace-trust = %d, %v", resp.StatusCode, err)
	}
	if got.Trust != config.WorkspaceTrusted || !got.Defaulted || ctrl.ToolApprovalMode() != control.ToolApprovalAuto {
		t.Fatalf("report %+v, mode %q", got, ctrl.ToolApprovalMode())
	}
}

func TestWorkspaceTrustRefusals(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	for _, tc := range []struct {
		name, root, body, code string
		status                 int
	}{
		{"unknown answer", testenv.TempDir(t), `{"trust":"maybe"}`, codeBadValue, http.StatusBadRequest},
		{"home directory", home, `{"trust":"trusted"}`, "workspace.untrustable", http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, ctrl, auth := trustServer(t, tc.root)
			status, code := postLaunchJSON(t, srv.URL+"/workspace-trust", tc.body, auth)
			if status != tc.status || code != tc.code {
				t.Fatalf("POST /workspace-trust = %d %q, want %d %q", status, code, tc.status, tc.code)
			}
			if ctrl.ToolApprovalMode() != control.ToolApprovalAsk {
				t.Fatalf("a refused answer moved the session to %q", ctrl.ToolApprovalMode())
			}
		})
	}
}
