package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
	"reasonix/internal/session/control"
)

func TestSkillCatalogProjectsAuthorInvocationFlags(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	here, there := testenv.TempDir(t), testenv.TempDir(t)
	cases := []struct {
		name, frontmatter, slashName string
		manual                       bool
	}{
		{"automatic", "", "automatic", false},
		{"manual", "invocation: manual\n", "manual", true},
		{"user-only", "disable-model-invocation: true\n", "user-only", true},
		{"model-only", "user-invocable: false\n", "", false},
		{"unreachable", "disable-model-invocation: true\nuser-invocable: false\n", "", true},
	}
	for _, root := range []string{here, there} {
		for _, tc := range cases {
			dir := filepath.Join(root, ".reasonix", "skills", tc.name)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			body := "---\nname: " + tc.name + "\ndescription: Invocation fixture\n" + tc.frontmatter + "---\nPerform the fixture task.\n"
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	store := skill.New(skill.Options{ProjectRoot: here, HomeDir: home, DisableBuiltins: true})
	if _, err := store.ForModel("user-only"); !errors.Is(err, skill.ErrModelInvocationDisabled) {
		t.Fatalf("user-only model invocation = %v, want the author's restriction", err)
	}
	if _, err := store.ForModel("model-only"); err != nil {
		t.Fatalf("model-only invocation = %v", err)
	}
	ctrl := control.New(control.Options{WorkspaceRoot: here, SkillStore: store, AllSkillStore: store})
	defer ctrl.Close()
	rememberWorkspace(there)
	t.Cleanup(func() { forgetWorkspace(there) })
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	for _, scope := range []struct{ name, query string }{
		{"current project", ""},
		{"other project", "?root=" + url.QueryEscape(there)},
	} {
		t.Run(scope.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + "/skills" + scope.query)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /skills = %d, want 200", resp.StatusCode)
			}
			var got struct {
				Skills []skillEntry `json:"skills"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			byName := make(map[string]skillEntry)
			for _, entry := range got.Skills {
				byName[entry.Name] = entry
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					entry, found := byName[tc.name]
					if !found || !entry.Enabled {
						t.Fatalf("author's skill missing or disabled: %+v", entry)
					}
					if entry.SlashName != tc.slashName {
						t.Errorf("slashName = %q, want %q", entry.SlashName, tc.slashName)
					}
					if entry.Manual != tc.manual {
						t.Errorf("manual = %v, want %v", entry.Manual, tc.manual)
					}
				})
			}
		})
	}
	for _, entry := range fetchSlash(t, ctrl) {
		if entry.Name == "model-only" || entry.Name == "unreachable" {
			t.Errorf("user-invocable:false skill appeared in /slash: %+v", entry)
		}
	}
	if ctrl.WorkspaceRoot() != here {
		t.Fatalf("inspecting another project's skills moved the running workspace to %q", ctrl.WorkspaceRoot())
	}
}
