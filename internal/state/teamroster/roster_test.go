package teamroster

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func agent(name string, main bool) Agent {
	return Agent{ID: newAgentID(), Name: name, Main: main, CreatedBy: ByUser, CreatedAt: time.Unix(1, 0)}
}

func team(t *testing.T) Roster {
	t.Helper()
	r := Roster{SchemaVersion: SchemaVersion, Agents: []Agent{agent("Chief", true), agent("写手", false), agent("Coder", false)}}
	r.Groups = []Group{{ID: newGroupID(), Name: "Launch", Members: []string{r.Agents[0].ID, r.Agents[1].ID}, Admin: r.Agents[0].ID, Mode: ModeAdmin, CreatedBy: ByUser}}
	if err := r.Validate(); err != nil {
		t.Fatalf("baseline roster invalid: %v", err)
	}
	return r
}

func TestValidateRefusesEachBrokenRule(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*Roster)
		want  error
		field string
	}{
		{"empty name", func(r *Roster) { r.Agents[1].Name = "" }, ErrEmpty, "name"},
		{"name past 40 runes", func(r *Roster) { r.Agents[1].Name = strings.Repeat("写", MaxNameRunes+1) }, ErrTooLong, "name"},
		{"name with a newline", func(r *Roster) { r.Agents[1].Name = "a\nb" }, ErrInvalid, "name"},
		{"name taken ignoring case", func(r *Roster) { r.Agents[2].Name = "chief" }, ErrNameTaken, "name"},
		{"role past 24 runes", func(r *Roster) { r.Agents[1].Role = strings.Repeat("r", MaxRoleRunes+1) }, ErrTooLong, "role"},
		{"instructions too long", func(r *Roster) { r.Agents[1].Instructions = strings.Repeat("i", MaxInstructionsRunes+1) }, ErrTooLong, "instructions"},
		{"avatar token", func(r *Roster) { r.Agents[1].Avatar.Glyph = "../x" }, ErrInvalid, "avatar"},
		{"agent id shape", func(r *Roster) { r.Agents[1].ID = "../../etc" }, ErrInvalid, "agent.id"},
		{"duplicate agent id", func(r *Roster) { r.Agents[2].ID = r.Agents[1].ID }, ErrInvalid, "agent.id"},
		{"no main agent", func(r *Roster) { r.Agents[0].Main = false }, ErrLastMain, ""},
		{"group of one", func(r *Roster) { r.Groups[0].Members = r.Groups[0].Members[:1] }, ErrGroupSize, ""},
		{"group past twelve", func(r *Roster) {
			for range MaxMembers {
				a := agent(newAgentID(), false)
				r.Agents = append(r.Agents, a)
				r.Groups[0].Members = append(r.Groups[0].Members, a.ID)
			}
		}, ErrGroupSize, ""},
		{"unknown member", func(r *Roster) { r.Groups[0].Members[1] = newAgentID() }, ErrUnknownAgent, ""},
		{"repeated member", func(r *Roster) { r.Groups[0].Members[1] = r.Groups[0].Members[0] }, ErrInvalid, "members"},
		{"admin outside the group", func(r *Roster) { r.Groups[0].Admin = r.Agents[2].ID }, ErrNotMember, ""},
		{"admin mode without an admin", func(r *Roster) { r.Groups[0].Admin = "" }, ErrInvalid, "mode"},
		{"unknown mode", func(r *Roster) { r.Groups[0].Mode = "loud" }, ErrInvalid, "mode"},
		{"relay limit past the bound", func(r *Roster) { r.Groups[0].RelayLimit = MaxRelayLimit + 1 }, ErrInvalid, "relay_limit"},
		{"session of a non-member", func(r *Roster) { r.Groups[0].MemberSessions = map[string]string{r.Agents[2].ID: "x.jsonl"} }, ErrNotMember, ""},
		{"group name taken", func(r *Roster) {
			g := r.Groups[0]
			g.ID = newGroupID()
			g.Name = "launch"
			r.Groups = append(r.Groups, g)
		}, ErrNameTaken, "group_name"},
		{"group id shape", func(r *Roster) { r.Groups[0].ID = "g-1" }, ErrInvalid, "group.id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := team(t)
			tc.edit(&r)
			err := r.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
			var fe *FieldError
			switch {
			case errors.As(err, &fe) && fe.Field != tc.field:
				t.Fatalf("field = %q, want %q", fe.Field, tc.field)
			case fe == nil && tc.field != "":
				t.Fatalf("no FieldError for %q in %v", tc.field, err)
			}
		})
	}
}

func TestValidateAcceptsTeamsTheProductAllows(t *testing.T) {
	r := team(t)
	unnamed := Group{ID: newGroupID(), Members: []string{r.Agents[1].ID, r.Agents[2].ID}, Mode: ModeEveryone, CreatedBy: r.Agents[0].ID}
	other := unnamed
	other.ID = newGroupID()
	r.Groups = append(r.Groups, unnamed, other)
	if err := r.Validate(); err != nil {
		t.Fatalf("two unnamed everyone-mode groups: %v", err)
	}
	if err := (&Roster{}).Validate(); err != nil {
		t.Fatalf("empty roster: %v", err)
	}
}

func TestLookupsAddressByIDAndExactName(t *testing.T) {
	r := team(t)
	if a, ok := r.AgentNamed("  CHIEF "); !ok || a.ID != r.Agents[0].ID {
		t.Fatalf("AgentNamed = %v, %v", a, ok)
	}
	if _, ok := r.AgentNamed("Chie"); ok {
		t.Fatal("a prefix matched a name")
	}
	if a, ok := r.Agent(r.Agents[2].ID); !ok || a.Name != "Coder" {
		t.Fatalf("Agent = %v, %v", a, ok)
	}
	g, ok := r.Group(r.Groups[0].ID)
	if !ok || !g.IsMember(r.Agents[1].ID) || g.IsMember(r.Agents[2].ID) {
		t.Fatalf("Group membership wrong: %+v", g)
	}
}

func TestAddAssignsFreshIDsTheCallerCannotChoose(t *testing.T) {
	r := team(t)
	a := r.AddAgent(Agent{ID: "a-0000000000000000", Name: "Planner", CreatedBy: ByUser})
	g := r.AddGroup(Group{ID: "g-0000000000000000", Members: []string{r.Agents[0].ID, a.ID}, Mode: ModeEveryone, CreatedBy: ByUser})
	if a.ID == "a-0000000000000000" || g.ID == "g-0000000000000000" {
		t.Fatalf("caller-chosen ids kept: %s %s", a.ID, g.ID)
	}
	if stored, ok := r.Agent(a.ID); !ok || stored.Name != "Planner" {
		t.Fatalf("added agent not found: %v", ok)
	}
	if _, ok := r.Group(g.ID); !ok {
		t.Fatal("added group not found")
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("roster after adds: %v", err)
	}
}

func TestAnAgentsFolderAndPermissionAreHeldToTheirShape(t *testing.T) {
	r := team(t)
	r.Agents[1].Folder, r.Agents[1].Permission = "/work/launch", "auto"
	if err := r.Validate(); err != nil {
		t.Fatalf("a valid folder and permission: %v", err)
	}
	for field, edit := range map[string]func(*Agent){
		"folder":     func(a *Agent) { a.Folder = "relative/dir" },
		"permission": func(a *Agent) { a.Permission = "root" },
	} {
		rr := team(t)
		edit(&rr.Agents[1])
		var fe *FieldError
		if err := rr.Validate(); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: %v", field, err)
		}
	}
	rr := team(t)
	rr.Agents[1].Folder = "/work/../etc"
	if err := rr.Validate(); err == nil {
		t.Error("an unclean folder path was accepted")
	}
}
