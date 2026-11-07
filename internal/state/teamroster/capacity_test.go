package teamroster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRosterHasFiniteAgentAndGroupCapacity(t *testing.T) {
	for _, kind := range []string{"agents", "groups"} {
		t.Run(kind, func(t *testing.T) {
			r := team(t)
			if kind == "agents" {
				for len(r.Agents) < 33 {
					r.AddAgent(agent(fmt.Sprintf("Member %d", len(r.Agents)), false))
				}
			} else {
				for len(r.Groups) < 17 {
					g := r.Groups[0]
					g.ID, g.Name = newGroupID(), fmt.Sprintf("Group %d", len(r.Groups))
					r.Groups = append(r.Groups, g)
				}
			}
			if err := r.Validate(); err == nil {
				t.Fatalf("unbounded %s accepted", kind)
			}
		})
	}
}

func TestLegacyOversizedRosterCanShrinkButCannotGrow(t *testing.T) {
	r := team(t)
	for len(r.Agents) < 34 {
		r.AddAgent(agent(fmt.Sprintf("Member %d", len(r.Agents)), false))
	}
	s := Store{Dir: t.TempDir()}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(); err != nil || len(got.Agents) != 34 {
		t.Fatalf("legacy read: %d, %v", len(got.Agents), err)
	}
	if _, err := s.Update(context.Background(), func(r *Roster) error { r.AddAgent(agent("More", false)); return nil }); !errors.Is(err, ErrAgentCapacity) {
		t.Fatalf("growth: %v", err)
	}
	if got, err := s.Update(context.Background(), func(r *Roster) error { r.Agents = r.Agents[:33]; return nil }); err != nil || len(got.Agents) != 33 {
		t.Fatalf("shrink: %d, %v", len(got.Agents), err)
	}
}

func TestRoleAndNameRejectTerminalControls(t *testing.T) {
	for _, control := range []string{"\x1b[2J", "\x00", "\x7f", "\u009b31m"} {
		for _, field := range []string{"name", "role"} {
			r := team(t)
			if field == "name" {
				r.Agents[1].Name = "Ada" + control
			} else {
				r.Agents[1].Role = "Writer" + control
			}
			if err := r.Validate(); err == nil {
				t.Errorf("%s accepted %q", field, control)
			}
		}
	}
}
