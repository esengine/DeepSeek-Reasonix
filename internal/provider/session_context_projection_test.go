package provider

import "testing"

func TestProjectionMessagesKeepsOriginThatModelMessagesStrips(t *testing.T) {
	stored := []Message{{Role: RoleUser, Origin: MessageOriginHost, Content: "host protocol"}}
	projection := ProjectionMessages(stored)
	if len(projection) != 1 || projection[0].Origin != MessageOriginHost {
		t.Fatalf("ProjectionMessages origin = %+v", projection)
	}
	model := ModelMessages(projection)
	if len(model) != 1 || model[0].Origin != "" || model[0].Content != "host protocol" {
		t.Fatalf("ModelMessages = %+v, want same content without provenance", model)
	}
}
