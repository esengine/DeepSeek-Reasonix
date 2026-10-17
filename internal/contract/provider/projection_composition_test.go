package provider

import (
	"strings"
	"testing"

	"reasonix/internal/contract/agentcomposition"
)

func TestCompositionMetadataAloneRequiresAModelProjection(t *testing.T) {
	binding := &agentcomposition.Binding{}
	stored := []Message{{Role: RoleSystem, Content: "BASE", AgentComposition: binding}}
	model := ModelMessages(stored)
	if model[0].AgentComposition != nil || model[0].Content != "BASE" {
		t.Fatal("local composition metadata reached the model projection")
	}
	if stored[0].AgentComposition != binding || ProjectionMessages(stored)[0].AgentComposition != binding {
		t.Fatal("projection changed the authoritative stored binding")
	}
}

func TestCompositionMetadataDoesNotHideAnsweredVideoProjection(t *testing.T) {
	binding := &agentcomposition.Binding{}
	stored := []Message{
		{Role: RoleSystem, Content: "BASE", AgentComposition: binding},
		videoMsg("v1"),
		{Role: RoleAssistant, Content: "a dog"},
	}
	model := ModelMessages(stored)
	if model[0].AgentComposition != nil || len(model[1].Videos) != 0 || !strings.Contains(model[1].Content, "is not attached again") {
		t.Fatal("combined projection leaked a binding or resent an answered video")
	}
	projection := ProjectionMessages(stored)
	if projection[0].AgentComposition != binding || len(projection[1].Videos) != 1 || len(stored[1].Videos) != 1 {
		t.Fatal("stored projection lost a binding or video reference")
	}
}
