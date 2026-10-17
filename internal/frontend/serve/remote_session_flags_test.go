package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteSessionFlagsUseFarKernel(t *testing.T) {
	for _, action := range []string{"pin", "archive"} {
		for _, value := range []bool{true, false} {
			t.Run(action+map[bool]string{true: "/on", false: "/off"}[value], func(t *testing.T) {
				flag := map[string]string{"pin": "pinned", "archive": "archived"}[action]
				calls, releases := 0, 0
				refused := false
				far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.Path != "/tree/sessions/"+action || body["path"] != "/same/session.jsonl" || body[flag] != value {
						t.Errorf("far request = %s %s %+v", r.Method, r.URL.Path, body)
					}
					if refused {
						refuse(w, http.StatusConflict, "session.running", "stop the conversation first", nil)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				defer far.Close()
				front := bookServer(t, &stubAttacher{attach: func(host, _ string) (RemoteEndpoint, func(), error) {
					if host != "gpu" {
						t.Errorf("host = %q", host)
					}
					return RemoteEndpoint{Host: host, Addr: far.Listener.Addr().String(), Token: "test"}, func() { releases++ }, nil
				}})
				body, _ := json.Marshal(map[string]any{"path": "/same/session.jsonl", flag: value})
				resp := bookPost(t, front, "/remotes/gpu/sessions/"+action, string(body))
				if resp.StatusCode != http.StatusNoContent || calls != 1 || releases != 1 {
					t.Fatalf("status=%d far calls=%d releases=%d", resp.StatusCode, calls, releases)
				}
				refused = true
				resp = bookPost(t, front, "/remotes/gpu/sessions/"+action, string(body))
				var reason Reason
				if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusConflict || reason.Code != "session.running" || releases != 2 {
					t.Fatalf("refusal=%d %+v releases=%d", resp.StatusCode, reason, releases)
				}
			})
		}
	}
}

func TestRemoteSessionFlagsDoNotDialDisabledHosts(t *testing.T) {
	front := bookServer(t, &stubAttacher{attach: func(_, _ string) (RemoteEndpoint, func(), error) {
		t.Fatal("disabled host was dialed")
		return RemoteEndpoint{}, nil, nil
	}})
	bookPost(t, front, "/remotes", `{"name":"gpu","host":"host","disabled":true}`)
	for _, action := range []string{"pin", "archive"} {
		resp := bookPost(t, front, "/remotes/gpu/sessions/"+action, `{"path":"/same/session.jsonl","pinned":true,"archived":true}`)
		var reason Reason
		if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusConflict || reason.Code != "remote.disabled" {
			t.Fatalf("%s = %d %+v", action, resp.StatusCode, reason)
		}
	}
}
