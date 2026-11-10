package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/memory"
)

func memoryReviewServer(t *testing.T, facts ...memory.Memory) *Server {
	t.Helper()
	set := memory.Load(memory.Options{CWD: testenv.TempDir(t), UserDir: testenv.TempDir(t)})
	for _, f := range facts {
		if _, err := set.Store.Save(f); err != nil {
			t.Fatal(err)
		}
	}
	c := control.New(control.Options{Memory: memory.Load(set.LoadOptions())})
	t.Cleanup(c.Close)
	return &Server{ctrl: c}
}

func listedMemories(t *testing.T, s *Server) map[string]memoryEntry {
	t.Helper()
	rec := httptest.NewRecorder()
	s.memories(rec, httptest.NewRequest(http.MethodGet, "/memory", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	var catalog struct {
		Memories []memoryEntry `json:"memories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	out := map[string]memoryEntry{}
	for _, m := range catalog.Memories {
		out[m.Name] = m
	}
	return out
}

func postMemory(s *Server, handler func(http.ResponseWriter, *http.Request), path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestTheMemoryListCarriesGalaxyStrengthAndNewness(t *testing.T) {
	s := memoryReviewServer(t,
		memory.Memory{Name: "deploy", Body: "deploys go through the platform branch", Galaxy: "release"},
		memory.Memory{Name: "rule", Body: "never force push", Type: memory.TypeConstraint},
	)
	got := listedMemories(t, s)
	deploy := got["deploy"]
	if deploy.Galaxy != "release" || deploy.Importance != memory.ImportanceNormal || deploy.State != "clear" {
		t.Fatalf("deploy = %+v, want its galaxy, normal importance and a clear state", deploy)
	}
	if !deploy.New || deploy.ClearForDays <= 0 || deploy.NoDecay {
		t.Fatalf("a fresh fact must read as new, decaying, with clear days left: %+v", deploy)
	}
	rule := got["rule"]
	if !rule.NoDecay || rule.ClearForDays != 0 || rule.Importance != memory.ImportanceHigh {
		t.Fatalf("a constraint must not decay and defaults to high importance: %+v", rule)
	}
}

func TestConfirmingAMemoryMarksItReviewedAndReinforcesIt(t *testing.T) {
	s := memoryReviewServer(t, memory.Memory{Name: "deploy", Body: "deploys go through the platform branch"})
	if rec := postMemory(s, s.confirmMemory, "/memory/confirm", `{"name":"deploy"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body)
	}
	deploy := listedMemories(t, s)["deploy"]
	if deploy.New || deploy.Uses != 1 || deploy.LastUsedAt == "" {
		t.Fatalf("after confirm = %+v, want reviewed with one use", deploy)
	}
	rec := postMemory(s, s.confirmMemory, "/memory/confirm", `{"name":"missing"}`)
	var reason Reason
	if err := json.Unmarshal(rec.Body.Bytes(), &reason); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound || reason.Code != "request.not_found" {
		t.Fatalf("unknown confirm = %d %+v, want 404 request.not_found", rec.Code, reason)
	}
	if rec := postMemory(s, s.confirmMemory, "/memory/confirm", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("confirm without a name = %d", rec.Code)
	}
}

func TestSavingAMemoryMovesItsGalaxyImportanceAndType(t *testing.T) {
	s := memoryReviewServer(t, memory.Memory{Name: "deploy", Body: "deploys go through the platform branch", Galaxy: "release"})
	body := `{"name":"deploy","body":"deploys go through the platform branch","galaxy":"  ops  ","importance":3,"type":"constraint"}`
	if rec := postMemory(s, s.saveMemory, "/memory/save", body); rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body)
	}
	deploy := listedMemories(t, s)["deploy"]
	if deploy.Galaxy != "ops" || deploy.Importance != 3 || deploy.Type != "constraint" || deploy.New {
		t.Fatalf("after save = %+v, want galaxy ops, importance 3, a constraint, reviewed", deploy)
	}
	if rec := postMemory(s, s.saveMemory, "/memory/save", `{"name":"deploy","body":"x"}`); rec.Code != http.StatusOK {
		t.Fatalf("save without galaxy = %d %s", rec.Code, rec.Body)
	}
	if got := listedMemories(t, s)["deploy"].Galaxy; got != "ops" {
		t.Fatalf("a save that names no galaxy moved it to %q", got)
	}
	for _, bad := range []string{`{"name":"deploy","body":"x","importance":7}`, `{"name":"deploy","body":"x","type":"rumour"}`} {
		if rec := postMemory(s, s.saveMemory, "/memory/save", bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("save %s = %d, want 400", bad, rec.Code)
		}
	}
}
