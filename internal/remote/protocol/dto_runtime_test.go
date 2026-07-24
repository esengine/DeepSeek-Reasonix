package protocol

import "testing"

func TestPendingPromptValidateCapabilityApproval(t *testing.T) {
	// capability_approval with approval payload: valid
	p := PendingPrompt{Kind: PromptCapabilityApproval, Approval: &ApprovalPrompt{
		PromptID: "cap-1", Tool: "bash", Subject: "pip install",
	}}
	if err := p.Validate(); err != nil {
		t.Fatalf("capability_approval with approval payload: %v", err)
	}

	// capability_approval with ask payload: invalid
	p2 := PendingPrompt{Kind: PromptCapabilityApproval, Ask: &AskPrompt{
		PromptID: "cap-1", Questions: []AskQuestion{{QuestionID: "q1", Options: []AskOption{{Label: "yes"}}}},
	}}
	if err := p2.Validate(); err == nil {
		t.Fatal("capability_approval with ask payload: expected error")
	}

	// capability_approval with both: invalid
	p3 := PendingPrompt{Kind: PromptCapabilityApproval, Approval: &ApprovalPrompt{PromptID: "cap-1", Tool: "bash", Subject: "test"},
		Ask: &AskPrompt{PromptID: "cap-1", Questions: []AskQuestion{{QuestionID: "q1"}}}}
	if err := p3.Validate(); err == nil {
		t.Fatal("capability_approval with both payloads: expected error")
	}

	// capability_approval with neither: invalid
	p4 := PendingPrompt{Kind: PromptCapabilityApproval}
	if err := p4.Validate(); err == nil {
		t.Fatal("capability_approval with no payload: expected error")
	}
}
