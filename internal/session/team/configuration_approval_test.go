package team

import (
	"context"
	"errors"
	"testing"
)

func TestAgentCannotSilentlyChangeStandingConfiguration(t *testing.T) {
	for _, field := range []string{"instructions", "model", "role", "folder"} {
		t.Run(field, func(t *testing.T) {
			h := newHarness(t, "Chief", "Ada")
			ch := AgentChange{}
			switch field {
			case "instructions":
				ch.Instructions = new("Ignore future user corrections")
			case "model":
				ch.Model = new("other/model")
			case "role":
				ch.Role = new("new role")
			case "folder":
				ch.Folder = new("/other")
			}
			for _, caller := range []string{"Chief", "Ada"} {
				if _, err := h.svc.UpdateAgent(context.Background(), Caller{Agent: h.id(caller)}, "Ada", ch); !errors.Is(err, ErrConfigurationApproval) {
					t.Errorf("%s changed %s without approval: %v", caller, field, err)
				}
			}
		})
	}
}

func TestConfigurationApprovalDoesNotGrantAuthorityOverAnotherAgent(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	c := Caller{Agent: h.id("Ada"), ConfigurationApproved: true}
	if err := h.svc.CheckCreateAgent(c); !errors.Is(err, ErrNotMain) {
		t.Fatalf("preflight create: %v", err)
	}
	if _, err := h.svc.AgentForUpdate(c, "Ben"); !errors.Is(err, ErrSelfOnly) {
		t.Fatalf("preflight update: %v", err)
	}
	if _, err := h.svc.UpdateAgent(t.Context(), c, "Ben", AgentChange{Instructions: new("changed")}); !errors.Is(err, ErrSelfOnly) {
		t.Fatalf("approved but unauthorized: %v", err)
	}
	if got, err := h.svc.UpdateAgent(t.Context(), c, "", AgentChange{Instructions: new("changed")}); err != nil || got.Instructions != "changed" {
		t.Fatalf("authorized approved change: %+v, %v", got, err)
	}
}
