package serve

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/secrets"
	"reasonix/internal/base/testenv"
	"reasonix/internal/session/team"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/teamroster"
	"reasonix/internal/state/usagereport"
)

type teamClient struct {
	t   *testing.T
	srv *httptest.Server
}

func (c teamClient) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var in io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		in = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, in)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (c teamClient) view(method, path string, body any) teamView {
	c.t.Helper()
	status, out := c.do(method, path, body)
	if status != http.StatusOK {
		c.t.Fatalf("%s %s = %d %s", method, path, status, out)
	}
	var v teamView
	if err := json.Unmarshal(out, &v); err != nil {
		c.t.Fatal(err)
	}
	return v
}

func (c teamClient) refused(method, path string, body any, status int, code string) Reason {
	c.t.Helper()
	got, out := c.do(method, path, body)
	var r Reason
	_ = json.Unmarshal(out, &r)
	if got != status || r.Code != code {
		c.t.Fatalf("%s %s = %d %s, want %d %s", method, path, got, out, status, code)
	}
	return r
}

func newTeamClient(t *testing.T, h *Hub) teamClient {
	srv := httptest.NewServer(operatorHandler(h))
	t.Cleanup(srv.Close)
	return teamClient{t: t, srv: srv}
}

func agentNamed(v teamView, name string) (teamAgentView, bool) {
	i := slices.IndexFunc(v.Agents, func(a teamAgentView) bool { return a.Name == name })
	if i < 0 {
		return teamAgentView{}, false
	}
	return v.Agents[i], true
}

func TestTheTeamRoutesCarryTheUsersEdits(t *testing.T) {
	h, _ := teamHub(t)
	c := newTeamClient(t, h)
	if status, out := c.do("GET", "/team", nil); status != 200 || !strings.Contains(string(out), `"agents":[]`) || !strings.Contains(string(out), `"groups":[]`) {
		t.Fatalf("an empty team = %d %s", status, out)
	}
	c.view("POST", "/team/bootstrap", map[string]string{"name": "Chief", "role": "Chief of Staff"})
	v := c.view("POST", "/team/bootstrap", map[string]string{"name": "Chief"})
	chief, ok := agentNamed(v, "Chief")
	if !ok || len(v.Agents) != 1 || !chief.Main || chief.Activity.State != "idle" {
		t.Fatalf("bootstrap twice = %+v", v.Agents)
	}
	v = c.view("POST", "/team/agents", map[string]any{"name": "Ada", "role": "Writer", "permission": "auto", "avatar": map[string]string{"glyph": "bunny", "color": "cat-2"}})
	ada, ok := agentNamed(v, "Ada")
	if !ok || ada.Permission != "auto" || ada.Avatar.Glyph != "bunny" || ada.Main {
		t.Fatalf("created = %+v", ada)
	}
	c.refused("PATCH", "/team/agents/"+chief.ID, map[string]any{"main": false}, 400, "team.last_main")
	r := c.refused("PATCH", "/team/agents/"+ada.ID, map[string]any{"name": strings.Repeat("x", teamroster.MaxNameRunes+1)}, 400, "team.too_long")
	if r.Params["field"] != "name" {
		t.Fatalf("a field error did not name its field: %+v", r.Params)
	}
	c.refused("PATCH", "/team/agents/a-0000000000000000", map[string]any{"pinned": true}, 404, "team.unknown_agent")
	if v := c.view("PATCH", "/team/agents/"+ada.ID, map[string]any{"pinned": true, "role": "Editor"}); !v.Agents[1].Pinned || v.Agents[1].Role != "Editor" {
		t.Fatalf("patched = %+v", v.Agents[1])
	}
}

func TestOpeningAnAgentShowsItsPaneAndClosingItHidesIt(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	c := newTeamClient(t, h)
	status, out := c.do("POST", "/team/agents/"+ids["Ada"]+"/open", nil)
	if status != 200 {
		t.Fatalf("open = %d %s", status, out)
	}
	var rt RuntimeView
	_ = json.Unmarshal(out, &rt)
	if !listed(h, rt.ID) {
		t.Fatal("an opened agent is not a pane")
	}
	if rt.Team == nil || rt.Team.Agent != ids["Ada"] || rt.Team.Group != "" {
		t.Fatalf("the pane does not say whose seat it is: %+v", rt.Team)
	}
	for _, v := range h.List() {
		if v.ID != rt.ID && v.Team != nil {
			t.Fatalf("a pane outside the team claims a seat: %+v", v)
		}
	}
	if v := c.view("GET", "/team", nil); v.Agents[1].Runtime != rt.ID {
		t.Fatalf("the view does not name the agent's pane: %+v", v.Agents[1])
	}
	if status, _ := c.do("POST", "/runtimes/"+rt.ID+"/close", nil); status != http.StatusNoContent {
		t.Fatalf("close = %d", status)
	}
	if listed(h, rt.ID) || h.Get(rt.ID) == nil {
		t.Fatal("closing the pane closed the agent's session instead of hiding it")
	}
}

// A member's group conversation is its own seat, and the pane says which.
func TestOpeningAMemberShowsItsGroupSeat(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	c := newTeamClient(t, h)
	g := c.view("POST", "/team/groups", map[string]any{"members": []string{ids["Chief"], ids["Ada"]}}).Groups[0].ID
	status, out := c.do("POST", "/team/groups/"+g+"/members/"+ids["Ada"]+"/open", nil)
	if status != 200 {
		t.Fatalf("open member = %d %s", status, out)
	}
	var rt RuntimeView
	_ = json.Unmarshal(out, &rt)
	if rt.Team == nil || rt.Team.Agent != ids["Ada"] || rt.Team.Group != g {
		t.Fatalf("seat = %+v, want Ada in %s", rt.Team, g)
	}
}

func TestAGroupTakesTheUsersMessages(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	c := newTeamClient(t, h)
	v := c.view("POST", "/team/groups", map[string]any{"name": "Launch", "members": []string{ids["Chief"], ids["Ada"]}, "admin": ids["Chief"]})
	if len(v.Groups) != 1 || v.Groups[0].Mode != "admin" || v.Groups[0].Round.Limit != 6 {
		t.Fatalf("group = %+v", v.Groups)
	}
	g := v.Groups[0].ID
	status, out := c.do("POST", "/team/groups/"+g+"/messages", map[string]any{"text": "plan the launch", "mentions": []string{ids["Ada"]}})
	if status != 200 || !strings.Contains(string(out), `"seq":1`) {
		t.Fatalf("post = %d %s", status, out)
	}
	status, out = c.do("GET", "/team/groups/"+g+"/log?limit=10", nil)
	var lines []teamroster.GroupLine
	_ = json.Unmarshal(out, &lines)
	if status != 200 || len(lines) != 1 || lines[0].Text != "plan the launch" || lines[0].Mentions[0] != ids["Ada"] {
		t.Fatalf("log = %d %s", status, out)
	}
	if v := c.view("GET", "/team", nil); v.Groups[0].Round.Speaking != ids["Ada"] || v.Groups[0].Preview != "plan the launch" {
		t.Fatalf("round = %+v", v.Groups[0])
	}
	c.refused("POST", "/team/groups/"+g+"/messages", map[string]any{"text": "  "}, 400, "team.invalid")
	if status, _ := c.do("POST", "/team/groups/"+g+"/continue", nil); status != http.StatusNoContent {
		t.Fatalf("continue = %d", status)
	}
}

func TestAReadOnlyHubShowsTheTeamButRefusesWrites(t *testing.T) {
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)
	opts := TeamHostOptions{Dir: testenv.TempDir(t), Workspace: testenv.TempDir(t)}
	release, err := (teamroster.Store{Dir: opts.Dir}).AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	h := NewHub(HubOptions{ProviderResolver: homeResolver(), Team: &opts})
	defer h.Shutdown()
	c := newTeamClient(t, h)
	if v := c.view("GET", "/team", nil); !v.ReadOnly {
		t.Fatal("a hub without the writer lease does not say it is read-only")
	}
	c.refused("POST", "/team/bootstrap", map[string]string{"name": "Chief"}, 409, "team.read_only")
}

// A window that opened while another still held the team — the one before a
// restart, still closing — takes the team over once that one lets go.
func TestAReadOnlyHubTakesTheTeamOverOnceTheHolderLetsGo(t *testing.T) {
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)
	opts := TeamHostOptions{Dir: testenv.TempDir(t), Workspace: testenv.TempDir(t)}
	release, err := (teamroster.Store{Dir: opts.Dir}).AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(HubOptions{ProviderResolver: homeResolver(), Team: &opts})
	defer h.Shutdown()
	c := newTeamClient(t, h)
	h.team.tick(time.Now())
	if v := c.view("GET", "/team", nil); !v.ReadOnly {
		t.Fatal("a tick took the lease while another still held it")
	}
	release()
	h.team.tick(time.Now())
	if v := c.view("GET", "/team", nil); v.ReadOnly {
		t.Fatal("the window stays read-only after the other holder let go")
	}
	if v := c.view("POST", "/team/bootstrap", map[string]string{"name": "Chief"}); len(v.Agents) != 1 {
		t.Fatalf("bootstrap after taking over = %+v", v.Agents)
	}
}

func TestAnAgentsUsageSumsItsChatAndGroupSeats(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	dir := testenv.TempDir(t)
	own, seat := filepath.Join(dir, "own.jsonl"), filepath.Join(dir, "seat.jsonl")
	for path, n := range map[string]int{own: 100, seat: 40} {
		if err := usagereport.Save(path, usagereport.Report{Version: usagereport.Version, Usage: usagereport.Usage{PromptTokens: n, CompletionTokens: n / 2, CacheHitTokens: n / 4, RequestCount: 2}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.team.store.Update(context.Background(), func(r *teamroster.Roster) error {
		a, _ := r.Agent(ids["Ada"])
		a.SessionPath = own
		g := r.AddGroup(teamroster.Group{Members: []string{ids["Chief"], ids["Ada"]}, Mode: teamroster.ModeEveryone, CreatedBy: teamroster.ByUser})
		gr, _ := r.Group(g.ID)
		gr.MemberSessions = map[string]string{ids["Ada"]: seat}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v := newTeamClient(t, h).view("GET", "/team", nil)
	ada, _ := agentNamed(v, "Ada")
	if ada.Usage != (teamUsageView{PromptTokens: 140, CompletionTokens: 70, CacheHitTokens: 35, Requests: 4}) {
		t.Fatalf("usage = %+v", ada.Usage)
	}
	if chief, _ := agentNamed(v, "Chief"); chief.Usage != (teamUsageView{}) {
		t.Fatalf("an agent with no conversations spent %+v", chief.Usage)
	}
}

// A photo is uploaded as a data URL, served from the team's own folder by a
// name the kernel chose, and taken off again; only a real picture is kept.
func TestAPhotoIsUploadedServedAndTakenOff(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	c := newTeamClient(t, h)
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64)
	body := map[string]string{"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(png))}
	ada, _ := agentNamed(c.view("POST", "/team/agents/"+ids["Ada"]+"/photo", body), "Ada")
	if !strings.HasPrefix(ada.Avatar.Image, "/team/avatars/") {
		t.Fatalf("avatar = %+v, want a served photo", ada.Avatar)
	}
	resp, err := http.Get(c.srv.URL + ada.Avatar.Image)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(got) != png || resp.Header.Get("Content-Type") != "image/png" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET photo = %d %q %v", resp.StatusCode, got, resp.Header)
	}

	svg := map[string]string{"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1)>"))}
	c.refused("POST", "/team/agents/"+ids["Ada"]+"/photo", svg, 400, "team.invalid")
	c.refused("POST", "/team/agents/"+ids["Ada"]+"/photo", map[string]string{"image": "/etc/passwd"}, 400, "team.invalid")

	worn := ada.Avatar.Image
	ada, _ = agentNamed(c.view("DELETE", "/team/agents/"+ids["Ada"]+"/photo", nil), "Ada")
	if ada.Avatar.Image != "" {
		t.Fatalf("after taking it off the avatar is %+v", ada.Avatar)
	}
	for _, path := range []string{worn, "/team/avatars/..%2Froster.json", "/team/avatars/roster.json"} {
		if status, _ := c.do("GET", path, nil); status == http.StatusOK {
			t.Fatalf("GET %s answered 200", path)
		}
	}
}

// The window lists what the team and a group share, and the person can make
// the team forget one; a write there is the person's, never an agent's.
func TestSharedMemoryIsListedAndForgottenFromTheWindow(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	c := newTeamClient(t, h)
	g := c.view("POST", "/team/groups", map[string]any{"name": "Launch", "members": []string{ids["Chief"], ids["Ada"]}}).Groups[0].ID
	svc := h.team.service()
	teamStore, _ := svc.SharedMemory(team.Caller{Agent: ids["Chief"]}, team.TierTeam, "", true)
	groupStore, _ := svc.SharedMemory(team.Caller{Agent: ids["Ada"], Group: g}, team.TierGroup, "", true)
	if _, err := teamStore.Save(memory.Memory{Name: "house-style", Description: "write in plain English", Type: memory.TypeFeedback, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := groupStore.Save(memory.Memory{Name: "launch-date", Description: "Friday", Type: memory.TypeProject, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	var shared []sharedMemoryView
	status, out := c.do("GET", "/team/memory?tier=team", nil)
	if err := json.Unmarshal(out, &shared); err != nil || status != 200 || len(shared) != 1 || shared[0].Name != "house-style" || shared[0].Description != "write in plain English" {
		t.Fatalf("team memory = %d %s", status, out)
	}
	status, out = c.do("GET", "/team/memory?tier=group&group="+g, nil)
	if err := json.Unmarshal(out, &shared); err != nil || status != 200 || len(shared) != 1 || shared[0].Name != "launch-date" {
		t.Fatalf("group memory = %d %s", status, out)
	}
	c.refused("GET", "/team/memory?tier=group&group=g-0000000000000000", nil, 404, "team.unknown_group")
	c.refused("GET", "/team/memory?tier=nobody", nil, 400, "team.invalid")
	if status, out := c.do("DELETE", "/team/memory/team/house-style", nil); status != http.StatusNoContent {
		t.Fatalf("forget = %d %s", status, out)
	}
	if _, out := c.do("GET", "/team/memory?tier=team", nil); string(out) != "[]\n" {
		t.Fatalf("after forgetting, team memory = %s", out)
	}
}

// The person sets, pauses and removes a team's scheduled tasks from the window.
func TestScheduledTasksAreKeptFromTheWindow(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	c := newTeamClient(t, h)
	status, out := c.do("POST", "/team/schedules", map[string]any{"name": "standup", "agent": ids["Ada"], "prompt": "summarise yesterday", "repeat": "daily", "clock": "09:00", "enabled": true})
	var sc teamroster.Schedule
	if err := json.Unmarshal(out, &sc); err != nil || status != http.StatusOK || sc.ID == "" || sc.Next.IsZero() {
		t.Fatalf("create = %d %s", status, out)
	}
	c.refused("POST", "/team/schedules", map[string]any{"name": "x", "agent": ids["Ada"], "prompt": "p", "repeat": "every", "every": 1, "unit": "minutes", "enabled": true}, 400, "team.invalid")
	status, out = c.do("PATCH", "/team/schedules/"+sc.ID, map[string]any{"name": "standup", "agent": ids["Ada"], "prompt": "summarise yesterday", "repeat": "daily", "clock": "09:00", "enabled": false})
	var paused teamroster.Schedule
	if err := json.Unmarshal(out, &paused); err != nil || status != http.StatusOK || paused.Enabled || !paused.Next.IsZero() {
		t.Fatalf("pause = %d %s", status, out)
	}
	var list []teamroster.Schedule
	if _, out := c.do("GET", "/team/schedules", nil); json.Unmarshal(out, &list) != nil || len(list) != 1 || list[0].Enabled {
		t.Fatalf("list = %s", out)
	}
	if status, _ := c.do("DELETE", "/team/schedules/"+sc.ID, nil); status != http.StatusNoContent {
		t.Fatalf("delete = %d", status)
	}
	c.refused("DELETE", "/team/schedules/"+sc.ID, nil, 404, "team.unknown_schedule")
}

// The window answers an agent's request for a secret and manages what it saved;
// nothing it reads back ever carries a value, and the file they live in is a
// path no session may read.
func TestSecretsAreAnsweredFromTheWindowAndNeverReadBack(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	c := newTeamClient(t, h)
	const value = "sk-shop-0123456789"
	defer secrets.UnregisterValue(value)
	if !slices.Contains(secrets.HostSecretPaths(), h.team.store.SecretPath()) {
		t.Fatal("the secrets file is not a host secret path")
	}
	if _, err := h.team.service().RequestSecret(context.Background(), team.Caller{Agent: ids["Ada"]}, "shop key", "the shop"); err != nil {
		t.Fatal(err)
	}
	_, out := c.do("GET", "/team/secrets", nil)
	if !strings.Contains(string(out), `"name":"SHOP_KEY"`) || !strings.Contains(string(out), ids["Ada"]) {
		t.Fatalf("pending request not listed: %s", out)
	}
	if status, out := c.do("POST", "/team/secrets", map[string]any{"name": "SHOP_KEY", "value": value, "purpose": "the shop", "agents": []string{ids["Ada"]}, "requester": ids["Ada"]}); status != http.StatusNoContent {
		t.Fatalf("save = %d %s", status, out)
	}
	_, out = c.do("GET", "/team/secrets", nil)
	if strings.Contains(string(out), value) || !strings.Contains(string(out), `"requests":[]`) || !strings.Contains(string(out), `"name":"SHOP_KEY"`) {
		t.Fatalf("after saving: %s", out)
	}
	c.refused("POST", "/team/secrets", map[string]any{"name": "PIN", "value": "12", "agents": []string{ids["Ada"]}}, 400, "team.invalid")
	if status, _ := c.do("DELETE", "/team/secrets/SHOP_KEY", nil); status != http.StatusNoContent {
		t.Fatalf("delete = %d", status)
	}
	c.refused("DELETE", "/team/secrets/SHOP_KEY", nil, 404, "team.unknown_secret")
}
