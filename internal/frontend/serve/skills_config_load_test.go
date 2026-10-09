package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
	"reasonix/internal/session/control"
)

// Listing the catalog costs one configuration read however many skills it
// holds: every read walks each installed plugin package.
func TestSkillsListReadsConfigurationOnce(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	reads := 0
	prev := config.SetInstalledPackages(func(string) []config.InstalledPackage {
		reads++
		return nil
	})
	t.Cleanup(func() { config.SetInstalledPackages(prev) })

	var skills []skill.Skill
	for i := range 40 {
		skills = append(skills, skill.Skill{Name: fmt.Sprintf("s%d", i), Scope: skill.ScopeProject})
	}
	ctrl := control.New(control.Options{Skills: skills, WorkspaceRoot: testenv.TempDir(t)})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	reads = 0
	resp, err := http.Get(srv.URL + "/skills")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if reads > 1 {
		t.Fatalf("GET /skills read the configuration %d times for 40 skills, want at most 1", reads)
	}
}
