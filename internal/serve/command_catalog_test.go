package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/skill"
)

func TestServeCommandsUsesActiveControllerCatalog(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Sink:     bc,
		Commands: []command.Command{{Name: "remote-review", Description: "Review remotely", ArgHint: "<scope>"}},
		Skills:   []skill.Skill{{Name: "remote-skill", Description: "Remote skill", RunAs: skill.RunSubagent}},
	})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/commands")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var entries []commandCatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]commandCatalogEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	if got := byName["remote-review"]; got.Kind != "custom" || got.Hint != "<scope>" {
		t.Fatalf("remote command = %+v", got)
	}
	if got := byName["remote-skill"]; got.Kind != "subagent" || got.Group != "subagents" {
		t.Fatalf("remote skill = %+v", got)
	}
	if byName["new"].Kind != "builtin" || byName["clear"].Kind != "builtin" {
		t.Fatalf("remote builtins missing: %+v", byName)
	}
	if byName["reload-cmd"].Kind != "builtin" {
		t.Fatalf("remote reload-cmd missing: %+v", byName["reload-cmd"])
	}
	if _, ok := byName["theme"]; ok {
		t.Fatal("remote catalog advertised the local-only /theme command")
	}
	if _, ok := byName["reload"]; ok {
		t.Fatal("remote catalog advertised the local-only /reload command")
	}
}
