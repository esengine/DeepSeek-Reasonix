package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// The line a Windows user writes by hand, and the file it leaves unreadable.
const brokenUserConfigBody = "default_model = \"existing/model-a\"\n\n" +
	"[[providers]]\nname = \"existing\"\nkind = \"openai\"\nbase_url = \"https://example.invalid/v1\"\n" +
	"models = [\"model-a\"]\ndefault = \"model-a\"\napi_key_env = \"EXISTING_API_KEY\"\n\n" +
	"[[plugins]]\nname = \"local\"\n" + `command = "C:\Scripts\mcp.exe"` + "\n"

// The line the parser stops at in the file above.
const brokenLine = 13

func breakUserConfig(t *testing.T) string {
	t.Helper()
	path := config.UserConfigPath()
	if err := os.WriteFile(path, []byte(brokenUserConfigBody), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Every settings panel writes to one file, so a file that will not parse is one
// condition with one code — not each panel's own sentence about columns. The
// sandbox panel is where this was reported; permissions answers the same.
func TestASaveIntoAnUnreadableConfigRefusesWithOneCode(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	breakUserConfig(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	for _, path := range []string{"/sandbox", "/permissions"} {
		resp := postProvider(t, srv.URL, path, `{"mode":"ask","deny":[],"ask":[],"allow":[],"bash":"off"}`)
		var body Reason
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict || body.Code != "config.unparsed" {
			t.Fatalf("POST %s = %d %q, want 409 config.unparsed", path, resp.StatusCode, body.Code)
		}
		if body.Params["line"] != float64(brokenLine) {
			t.Fatalf("params = %v, want the offending line in them", body.Params)
		}
		if repair, _ := body.Params["repair"].(string); repair == "" {
			t.Fatalf("params = %v, want the repair to offer", body.Params)
		}
	}
}

// The panel asks before it tries: showing recovered values without saying so is
// what made the screen a lie until someone touched it.
func TestTheProblemIsReadableBeforeAnythingIsSaved(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	breakUserConfig(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	problem := readJSON[*control.ConfigProblem](t, srv.URL, "/config/problem")
	if problem == nil || problem.Line != brokenLine {
		t.Fatalf("problem = %+v, want the offending line", problem)
	}
	if problem.Recovered == "" {
		t.Fatalf("problem = %+v, want it to say which values are on screen", problem)
	}
}

func TestRepairingTheFileLetsSettingsSaveAgain(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	breakUserConfig(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/config/repair", "")
	got, _ := readAllString(resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /config/repair = %d: %s", resp.StatusCode, got)
	}

	saved := postProvider(t, srv.URL, "/sandbox", `{"bash":"off","network":true,"allowWrite":[]}`)
	body, _ := readAllString(saved)
	saved.Body.Close()
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("POST /sandbox after repair = %d: %s", saved.StatusCode, body)
	}
	if problem := readJSON[*control.ConfigProblem](t, srv.URL, "/config/problem"); problem != nil {
		t.Fatalf("problem after repair = %+v, want none", problem)
	}
}

// The offending line is the file as written, and the line that stopped the
// parser may be the one holding a key. A server that does not let its client
// edit its config does not hand out its contents either.
func TestTheOffendingLineIsNotHandedToAnUngrantedClient(t *testing.T) {
	s := newProviderEditServer(t)
	breakUserConfig(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/config/problem")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /config/problem without the grant = %d, want 403", resp.StatusCode)
	}
}
