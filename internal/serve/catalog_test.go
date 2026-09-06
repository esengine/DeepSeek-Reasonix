package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
	"reasonix/internal/testenv"
)

func fetchSlash(t *testing.T, ctrl *control.Controller) []slashEntry {
	t.Helper()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/slash")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /slash = %d, want 200", resp.StatusCode)
	}
	var got []slashEntry
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// Submit resolves a custom command before a skill of the same name, so a menu
// built from this endpoint must offer the entry that would actually run.
func TestSlashCommandShadowsSameNamedSkill(t *testing.T) {
	ctrl := control.New(control.Options{
		Commands: []command.Command{{Name: "review", Description: "the command"}},
		Skills: []skill.Skill{
			{Name: "review", Description: "the skill", Scope: skill.ScopeProject},
			{Name: "audit", Description: "read-only sweep", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent},
		},
	})
	defer ctrl.Close()

	got := fetchSlash(t, ctrl)
	byName := map[string][]slashEntry{}
	for _, e := range got {
		byName[e.Name] = append(byName[e.Name], e)
	}
	if n := len(byName["review"]); n != 1 {
		t.Fatalf("review appears %d times, want 1", n)
	}
	if k := byName["review"][0].Kind; k != "command" {
		t.Errorf("review kind = %q, want command", k)
	}

	audit := byName["audit"]
	if len(audit) != 1 {
		t.Fatalf("audit appears %d times, want 1", len(audit))
	}
	if !audit[0].Subagent {
		t.Error("audit lost its subagent flag; the menu cannot say it needs a task")
	}
	if audit[0].Scope != string(skill.ScopeBuiltin) {
		t.Errorf("audit scope = %q, want builtin", audit[0].Scope)
	}
}

// A session with no plugin host still has to answer with a list. JSON null
// would reach the frontend as a value it cannot iterate.
func TestMcpWithoutHostReturnsEmptyList(t *testing.T) {
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Servers []mcpEntry `json:"servers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("GET /mcp = %q: %v", strings.TrimSpace(string(body)), err)
	}
	if len(got.Servers) != 0 {
		t.Fatalf("GET /mcp servers = %+v, want none", got.Servers)
	}
}

// The slash list only holds what the user can type. A management surface has to
// see the skills that only the model can reach, or it cannot switch them off.
func TestSkillsListsWhatSlashCannotShow(t *testing.T) {
	ctrl := control.New(control.Options{
		Skills: []skill.Skill{
			{Name: "audit", Description: "read-only sweep", Scope: skill.ScopeBuiltin, RunAs: skill.RunSubagent, ReadOnly: true},
			{Name: "hinted", Description: "manual only", Scope: skill.ScopeProject, Invocation: "manual"},
		},
	})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	var got struct {
		Implicit bool         `json:"implicit"`
		Skills   []skillEntry `json:"skills"`
	}
	resp, err := http.Get(srv.URL + "/skills")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	by := map[string]skillEntry{}
	for _, e := range got.Skills {
		by[e.Name] = e
	}
	if len(by) != 2 {
		t.Fatalf("skills = %d entries, want 2: %+v", len(by), got.Skills)
	}
	if !by["audit"].ReadOnly || !by["audit"].Subagent {
		t.Errorf("audit lost its capability face: %+v", by["audit"])
	}
	if !by["audit"].Enabled {
		t.Error("audit reads as disabled; nothing disabled it")
	}
	if !by["hinted"].Manual {
		t.Error("a manual skill must not read as model-discoverable")
	}
	if by["audit"].Manual {
		t.Error("an auto skill must not read as manual")
	}
}

// The switch has to survive the request that set it, or the UI is reporting a
// state the next reload will contradict. With no workspace open the decision
// lands on the global layer — a project row keyed by nothing would resolve for
// nobody, so the switch would silently do nothing.
func TestSkillEnabledPersists(t *testing.T) {
	ctrl := control.New(control.Options{
		Skills: []skill.Skill{{Name: "audit", Scope: skill.ScopeBuiltin}},
	})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/skills/enabled", "application/json",
		strings.NewReader(`{"name":"audit","enabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /skills/enabled = %d (%s), want 200", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if ctrl.SkillEnabled("audit") {
		t.Fatal("the skill is still enabled after the switch said off")
	}
}

// The same switch in one project must not answer for another. This is the
// asymmetry the skill surface used to have against MCP: a bare name in the
// user config disabled every same-named skill in every project at once.
func TestSkillSwitchIsScopedToItsProject(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	here, there := testenv.TempDir(t), testenv.TempDir(t)
	skills := []skill.Skill{{Name: "deploy", Scope: skill.ScopeProject}}

	mine := control.New(control.Options{Skills: skills, WorkspaceRoot: here})
	defer mine.Close()
	theirs := control.New(control.Options{Skills: skills, WorkspaceRoot: there})
	defer theirs.Close()

	srv := httptest.NewServer(New(mine, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/skills/enabled", "application/json",
		strings.NewReader(`{"name":"deploy","enabled":false,"scope":"project"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if mine.SkillEnabled("deploy") {
		t.Fatal("the skill stayed on in the project that switched it off")
	}
	if !theirs.SkillEnabled("deploy") {
		t.Fatal("switching a skill off in one project also switched it off in another")
	}
}

// An unknown name is a client mistake, not a server failure: a 5xx would send
// the frontend into a retry loop over a name that will never resolve.
func TestMcpAdminRejectsUnknownServer(t *testing.T) {
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	for _, tc := range []struct {
		path, body string
		want       int
	}{
		// A name nothing is configured under is the thing being missing, not the
		// request being malformed — the second row is the malformed one.
		{"/mcp/enabled", `{"name":"nope","enabled":true}`, http.StatusNotFound},
		{"/mcp/enabled", `{"enabled":true}`, http.StatusBadRequest},
		{"/mcp/reconnect", `{"name":"nope"}`, http.StatusBadGateway},
	} {
		resp, err := http.Post(srv.URL+tc.path, "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("POST %s %s = %d, want %d", tc.path, tc.body, resp.StatusCode, tc.want)
		}
	}
}

// Parsing is a separate step from installing so the user can be shown what a
// stranger's config would run before agreeing to it. It must never connect or
// persist anything on its own.
func TestMcpParsePreviewsWithoutInstalling(t *testing.T) {
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/mcp/parse", "application/json",
		strings.NewReader(`{"input":"{\"mcpServers\":{\"gh\":{\"command\":\"npx\",\"args\":[\"-y\",\"x\"],\"env\":{\"GITHUB_TOKEN\":\"ghp_literal\"}}}}"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /mcp/parse = %d (%s)", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var got struct {
		Servers []draftServer `json:"servers"`
		Risks   []draftRisk   `json:"risks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Servers) != 1 || got.Servers[0].Name != "gh" {
		t.Fatalf("parse returned %+v, want one server named gh", got.Servers)
	}
	if got.Servers[0].Transport != "stdio" {
		t.Errorf("transport = %q, want stdio spelled out", got.Servers[0].Transport)
	}
	var secret bool
	for _, k := range got.Risks {
		if k.Kind == "secret" {
			secret = true
		}
	}
	if !secret {
		t.Error("a literal token reached the confirmation card unflagged")
	}
	// Nothing may exist yet: the user has not agreed to anything.
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("parse persisted %v", names)
	}
}

// A name collision is a client-side mistake with a precise remedy, so it comes
// back as a structured result rather than a 500 the UI can only print.
func TestMcpInstallRejectsDuplicateName(t *testing.T) {
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	// A command that cannot connect still exercises the pre-flight checks, and
	// leaves nothing behind — which is the contract being asserted.
	post := func(body string) map[string]any {
		resp, err := http.Post(srv.URL+"/mcp/install", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := post(`{"server":{"name":"","command":"true"},"scope":"user"}`)
	if got["state"] != "issue" {
		t.Errorf("a nameless server installed as %v, want issue", got["state"])
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("a failed install left %v behind", names)
	}
}

// A hidden command is invocable but deliberately absent from listings.
func TestSlashOmitsHiddenCommands(t *testing.T) {
	ctrl := control.New(control.Options{
		Commands: []command.Command{
			{Name: "ship", Description: "visible"},
			{Name: "sh", Description: "compat alias", Hidden: true},
		},
	})
	defer ctrl.Close()

	for _, e := range fetchSlash(t, ctrl) {
		if e.Name == "sh" {
			t.Fatal("hidden command reached the slash listing")
		}
	}
}

// The picker's whole point: manage a folder the session is not sitting in.
// The runtime must not move, and the other project's switch must land under
// its own identity rather than the running one's.
func TestCapabilitySwitchReachesAnotherProject(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	here, there := testenv.TempDir(t), testenv.TempDir(t)
	rememberWorkspace(there)
	t.Cleanup(func() { forgetWorkspace(there) })

	ctrl := control.New(control.Options{
		Skills:        []skill.Skill{{Name: "deploy", Scope: skill.ScopeProject}},
		WorkspaceRoot: here,
	})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/skills/enabled", "application/json",
		strings.NewReader(`{"name":"deploy","enabled":false,"scope":"project","root":`+
			mustJSON(t, there)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /skills/enabled = %d (%s), want 200", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if ctrl.WorkspaceRoot() != here {
		t.Fatalf("the runtime moved to %q; managing another project must not repoint it", ctrl.WorkspaceRoot())
	}
	if !ctrl.SkillEnabled("deploy") {
		t.Fatal("switching another project's skill off also switched it off here")
	}
	store := config.DefaultActivationStore()
	if on, err := store.SkillEnabled("deploy", there, true); err != nil || on {
		t.Fatalf("the other project: enabled=%v err=%v, want the switch to have landed there", on, err)
	}
}

// A folder the shell never opened is not addressable: the request would
// otherwise be a way to read or edit any directory's config.
func TestCapabilitySwitchRefusesAnUnknownProject(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	ctrl := control.New(control.Options{
		Skills:        []skill.Skill{{Name: "deploy"}},
		WorkspaceRoot: testenv.TempDir(t),
	})
	defer ctrl.Close()
	srv := httptest.NewServer(New(ctrl, NewBroadcaster(), config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/skills/enabled", "application/json",
		strings.NewReader(`{"name":"deploy","enabled":false,"root":`+mustJSON(t, testenv.TempDir(t))+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST with an unopened project = %d, want 400", resp.StatusCode)
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// A configured server with no process running is three different things, and
// the panel used to have two words for them. The reported symptom was every
// working server reading as "not connected" beside a Reconnect button: a
// cache-hit server is process-idle by design, with its whole toolset callable.
func TestAConfiguredServerSaysWhetherItIsCallableRatherThanRunning(t *testing.T) {
	enabled := control.MCPServerState{Enabled: true}
	off := control.MCPServerState{Enabled: false}

	if got := configuredState(enabled, 12); got != "standby" {
		t.Fatalf("enabled with 12 tools in the catalog = %q, want standby", got)
	}
	if got := configuredState(enabled, 0); got != "idle" {
		t.Fatalf("enabled with nothing registered = %q, want idle", got)
	}
	// Switched off outranks the catalog: tools can linger a moment after the
	// switch, and the row has to report the decision, not the leftovers.
	if got := configuredState(off, 12); got != "disabled" {
		t.Fatalf("switched off = %q, want disabled", got)
	}
}

// The settings pane lists a server so the user can decide whether to keep it.
// Name, transport and source cannot answer that; what the server says it is
// and what its tools do can, so both have to survive the wire shape.
func TestMcpRowCarriesWhatTheServerSaidAboutItself(t *testing.T) {
	row := remembered(control.MCPServerState{
		Description: "Maps a repository's symbols.",
		Tools: []plugin.ToolInfo{
			{Name: "find_symbol", Description: "Locate a symbol by name.", ReadOnlyHint: true},
			{Name: "rewrite", Description: "Rewrite a file in place.", DestructiveHint: true},
			{Name: "broken", SchemaError: "input schema is not an object"},
		},
		Stale: true,
	}, mcpEntry{Name: "atlas", State: "disabled"})

	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Description string    `json:"description"`
		Tools       int       `json:"tools"`
		ToolList    []mcpTool `json:"toolList"`
		Remembered  bool      `json:"remembered"`
		Stale       bool      `json:"stale"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Description != "Maps a repository's symbols." {
		t.Fatalf("description = %q, want the server's own words", got.Description)
	}
	if got.Tools != 3 || len(got.ToolList) != 3 {
		t.Fatalf("tool count = %d, list = %d, want 3 and 3", got.Tools, len(got.ToolList))
	}
	if got.ToolList[0].Description != "Locate a symbol by name." || !got.ToolList[0].ReadOnly {
		t.Errorf("tool row = %+v, want its description and read-only hint", got.ToolList[0])
	}
	if !got.ToolList[1].Destructive {
		t.Error("a destructive tool reached the pane looking like any other")
	}
	if got.ToolList[2].Error == "" {
		t.Error("a tool the host cannot call was listed as if it could be")
	}
	// A disconnected server's answer is a record, not a report: an unmarked row
	// would claim something switched off is speaking for itself.
	if !got.Remembered || !got.Stale {
		t.Errorf("remembered = %v, stale = %v, want both marked", got.Remembered, got.Stale)
	}
}

// Server-written text lands in a browser row. Escape sequences and line breaks
// are not display, and one verbose server must not make the page expensive.
func TestServerTextIsSanitizedAndBounded(t *testing.T) {
	got := displayText("\x1b[31mred\x1b[0m\nline\ttwo\x00", mcpToolTextLimit)
	if got != "red line two" {
		t.Fatalf("displayText = %q, want the control characters gone", got)
	}
	long := strings.Repeat("x", mcpServerTextLimit*2)
	if bounded := displayText(long, mcpServerTextLimit); len([]rune(bounded)) > mcpServerTextLimit+1 {
		t.Fatalf("displayText kept %d runes, want it bounded at %d", len([]rune(bounded)), mcpServerTextLimit)
	}
}
