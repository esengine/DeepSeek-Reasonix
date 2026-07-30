package main

import (
	"testing"

	"reasonix/internal/command"
)

func TestDecoratePluginCommandConflicts(t *testing.T) {
	view := PluginView{
		Name:    "pwf",
		Enabled: true,
		CommandDetails: []PluginCommandView{
			{Name: "plan", Invocation: "/pwf:plan"},
			{Name: "status", Invocation: "/pwf:status"},
			{Name: "new", Invocation: "/pwf:new"},
		},
	}
	decoratePluginCommandConflicts(&view, []command.Command{
		{Name: "plan", Plugin: "pwf", ShortName: "plan", Hidden: true},
		{Name: "pwf:plan", Plugin: "pwf", ShortName: "plan"},
		{Name: "pwf:status", Description: "explicit command"},
	})

	plan := view.CommandDetails[0]
	if plan.Shadowed {
		t.Fatalf("canonical plan should be available: %+v", plan)
	}
	if status := view.CommandDetails[1]; !status.Shadowed || status.ShadowedByPlugin != "" {
		t.Fatalf("occupied canonical status = %+v", status)
	}
	if fresh := view.CommandDetails[2]; fresh.Shadowed {
		t.Fatalf("command absent from a stale active session must not be reported as overridden: %+v", fresh)
	}
}

func TestDecoratePluginCommandConflictNamesWinningPlugin(t *testing.T) {
	view := PluginView{Name: "alpha", Enabled: true, CommandDetails: []PluginCommandView{{Name: "plan"}}}
	decoratePluginCommandConflicts(&view, []command.Command{
		{Name: "alpha:plan", Plugin: "beta", ShortName: "alpha:plan"},
	})
	got := view.CommandDetails[0]
	if !got.Shadowed || got.ShadowedByPlugin != "beta" {
		t.Fatalf("plugin conflict = %+v", got)
	}
}
