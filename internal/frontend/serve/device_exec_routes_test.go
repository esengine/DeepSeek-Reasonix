package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pairedGate(t *testing.T, reached *int) (http.Handler, *http.Cookie) {
	t.Helper()
	reg := NewDeviceRegistry()
	code, _ := reg.Offer()
	cred, _, err := reg.Redeem(code, "phone")
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { *reached++ })
	gate := NewDeviceGate(next, DeviceGateOptions{Registry: reg, Origin: "http://192.168.1.5:9000"})
	return gate, &http.Cookie{Name: DeviceCookie, Value: cred}
}

func deviceCall(gate http.Handler, c *http.Cookie, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://192.168.1.5:9000"+path, strings.NewReader("{}"))
	req.Header.Set("Origin", "http://192.168.1.5:9000")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	return rec
}

func TestPairedDeviceCannotGrantStandingExecutionAuthority(t *testing.T) {
	for _, r := range []hostRoute{
		{http.MethodPost, "/plugins/install"},
		{http.MethodPost, "/plugins/enabled"},
		{http.MethodDelete, "/plugins/some-plugin"},
		{http.MethodPost, "/mcp/install"},
		{http.MethodPost, "/mcp/trust"},
		{http.MethodPost, "/mcp/enabled"},
		{http.MethodPost, "/mcp/load"},
		{http.MethodPost, "/mcp/follow"},
		{http.MethodPost, "/mcp/upgrade"},
		{http.MethodPost, "/hooks"},
		{http.MethodPost, "/market/install"},
		{http.MethodPost, "/market/mine/install"},
		{http.MethodPost, "/workspace-trust"},
		{http.MethodPost, "/team/bootstrap"},
		{http.MethodPost, "/team/agents"},
		{http.MethodPatch, "/team/agents/a-0123456789abcdef"},
		{http.MethodDelete, "/team/agents/a-0123456789abcdef"},
		{http.MethodPost, "/team/groups"},
		{http.MethodPatch, "/team/groups/g-0123456789abcdef"},
		{http.MethodDelete, "/team/groups/g-0123456789abcdef"},
		{http.MethodPatch, "/rt/abc123/team/agents/a-0123456789abcdef"},
		{http.MethodPatch, "/team/prefs"},
		{http.MethodPost, "/team/schedules"},
		{http.MethodPatch, "/team/schedules/s-0123456789abcdef"},
		{http.MethodDelete, "/team/schedules/s-0123456789abcdef"},
		{http.MethodPost, "/rt/abc123/plugins/install"},
		{http.MethodPost, "/rt/abc123/hooks"},
	} {
		reached := 0
		gate, cookie := pairedGate(t, &reached)
		rec := deviceCall(gate, cookie, r.method, r.path)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), codeDeviceHostOnly) || reached != 0 {
			t.Errorf("%s %s from a paired device: status %d body %q reached=%d, want 404 %s and never reached", r.method, r.path, rec.Code, rec.Body.String(), reached, codeDeviceHostOnly)
		}
	}
}

func TestPairedDeviceStillReadsAndPlansExtensions(t *testing.T) {
	for _, r := range []hostRoute{
		{http.MethodGet, "/plugins"},
		{http.MethodPost, "/plugins/plan"},
		{http.MethodPost, "/mcp/reconnect"},
		{http.MethodPost, "/submit"},
		{http.MethodPost, "/runtimes"},
		{http.MethodPost, "/tool-approval-mode"},
		{http.MethodGet, "/team"},
		{http.MethodPost, "/team/groups/g-0123456789abcdef/messages"},
		{http.MethodPost, "/team/agents/a-0123456789abcdef/stop"},
	} {
		reached := 0
		gate, cookie := pairedGate(t, &reached)
		rec := deviceCall(gate, cookie, r.method, r.path)
		if rec.Code == http.StatusNotFound || reached != 1 {
			t.Errorf("%s %s from a paired device: status %d reached=%d, want it served", r.method, r.path, rec.Code, reached)
		}
	}
}
