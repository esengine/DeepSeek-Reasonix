package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/session/team"
)

func TestPairedDeviceCannotAssignTeamWorkingFolders(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	client := newTeamClient(t, h)
	group := client.view("POST", "/team/groups", map[string]any{"members": []string{ids["Chief"], ids["Ada"]}}).Groups[0].ID
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/team/agents", `{"name":"Remote","folder":` + quoteJSON(t.TempDir()) + `}`},
		{"PATCH", "/team/agents/" + ids["Ada"], `{"folder":` + quoteJSON(t.TempDir()) + `}`},
		{"POST", "/team/groups", `{"members":[` + quoteJSON(ids["Chief"]) + `,` + quoteJSON(ids["Ada"]) + `],"folder":` + quoteJSON(t.TempDir()) + `}`},
		{"PATCH", "/team/groups/" + group, `{"folder":` + quoteJSON(t.TempDir()) + `}`},
	} {
		before, err := h.team.store.Load()
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(tc.method, "http://localhost"+tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(withDeviceReach(req.Context(), "phone", 1))
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), codeDeviceHostOnly) {
			t.Errorf("%s %s granted host authority: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		after, err := h.team.store.Load()
		if err != nil || before.Revision != after.Revision {
			t.Errorf("device request changed roster: revision %d -> %d, %v", before.Revision, after.Revision, err)
		}
	}
}

func TestReadingTeamViewDoesNotRefreshBackgroundActivity(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	rt := ensureSeat(t, h, team.SeatRef{Agent: ids["Ada"]})
	h.team.mu.Lock()
	before := h.team.holds[rt.ID].used
	h.team.mu.Unlock()
	if _, err := h.team.view(); err != nil {
		t.Fatal(err)
	}
	h.team.mu.Lock()
	after := h.team.holds[rt.ID].used
	h.team.mu.Unlock()
	if !before.Equal(after) {
		t.Fatalf("reading the sidebar refreshed idle activity: %s -> %s", before, after)
	}
	h.team.closeIdle(before.Add(teamIdleClose + time.Nanosecond))
	if h.Get(rt.ID) != nil {
		t.Fatal("sidebar refresh kept an idle background runtime alive")
	}
}
