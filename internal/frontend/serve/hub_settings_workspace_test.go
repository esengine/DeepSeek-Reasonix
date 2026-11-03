package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/state/memory"
)

func TestMachineSettingsSelectLocalWorkspace(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	resolver := homeResolver()
	h := NewHub(HubOptions{ProviderResolver: resolver})
	open := func(body, model string) *Runtime {
		t.Helper()
		root := testenv.TempDir(t)
		ctrl := control.New(control.Options{WorkspaceRoot: root, ModelRef: model, ProviderResolver: resolver,
			Memory: memory.Load(memory.Options{CWD: root, UserDir: home})})
		t.Cleanup(ctrl.Close)
		if _, err := ctrl.SaveMemory(memory.Memory{Name: "fact", Body: body, Type: memory.TypeProject}); err != nil {
			t.Fatal(err)
		}
		bc := NewBroadcaster()
		rt, err := h.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
		if err != nil {
			t.Fatal(err)
		}
		return rt
	}
	first, second := open("first", "home/chat-a"), open("second", "home/chat-b")
	handler := operatorHandler(h)
	request := func(id, method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		base := "/host/settings"
		if id != "" {
			base += "/runtime/" + id
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://localhost"+base+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s%s = %d %s, want %d", method, base, path, w.Code, w.Body.String(), status)
		}
		return w
	}
	read := func(id, want string) {
		t.Helper()
		w := request(id, http.MethodGet, "/memory", "", http.StatusOK)
		var body struct{ Memories []memoryEntry }
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Memories) != 1 || body.Memories[0].Body != want {
			t.Fatalf("%s memories = %+v, want %q", id, body.Memories, want)
		}
	}
	read("", "first")
	read(second.ID, "second")
	unauthorized := httptest.NewRequest(http.MethodPost, "http://localhost/host/settings/runtime/"+second.ID+"/memory/save", strings.NewReader(`{"name":"fact","body":"unauthorized"}`))
	unauthorized.Header.Set("Content-Type", "application/json")
	denied := httptest.NewRecorder()
	h.Handler().ServeHTTP(denied, unauthorized)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unauthorized workspace mutation = %d %s", denied.Code, denied.Body.String())
	}
	read(second.ID, "second")
	w := request(second.ID, http.MethodGet, "/models", "", http.StatusOK)
	var models resolverModelsBody
	if err := json.Unmarshal(w.Body.Bytes(), &models); err != nil || models.Current != "home/chat-b" {
		t.Fatalf("second models = %s, error %v", w.Body.String(), err)
	}
	request(second.ID, http.MethodPost, "/memory/save", `{"name":"fact","body":"edited"}`, http.StatusOK)
	read(second.ID, "edited")
	read(first.ID, "first")
	w = request(second.ID, http.MethodGet, "/memory/revisions?name=fact", "", http.StatusOK)
	if !strings.Contains(w.Body.String(), `"body":"second"`) || strings.Contains(w.Body.String(), `"body":"first"`) {
		t.Fatalf("second revisions = %s", w.Body.String())
	}
	request(second.ID, http.MethodPost, "/memory/restore", `{"name":"fact","revision":1}`, http.StatusOK)
	read(second.ID, "second")
	request(second.ID, http.MethodPost, "/memory/forget", `{"name":"fact"}`, http.StatusNoContent)
	got, err := second.Server.Controller().Memory().Store.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("forgotten second memory = %+v", got)
	}
	read(first.ID, "first")
	far := fakeRemoteKernel(t)
	remote := remotePane(t, h, far.Listener.Addr().String())
	if err := h.Close(second.ID); err != nil {
		t.Fatal(err)
	}
	replacement := open("replacement", "home/chat-b")
	read(replacement.ID, "replacement")
	for _, id := range []string{second.ID, remote.ID, "missing"} {
		for _, path := range []string{"/memory/save", "/memory/forget", "/memory/confirm", "/memory/restore"} {
			w := request(id, http.MethodPost, path, `{"name":"fact","body":"wrong","revision":1}`, http.StatusServiceUnavailable)
			if !strings.Contains(w.Body.String(), `"settings.local_runtime_required"`) {
				t.Fatalf("invalid owner %s: %s", id, w.Body.String())
			}
		}
		request(id, http.MethodGet, "/models", "", http.StatusServiceUnavailable)
	}
	read("", "first")
	read(replacement.ID, "replacement")
	select {
	case cookie := <-far.cookies:
		t.Fatalf("local settings reached remote: %s", cookie)
	default:
	}
}
