package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/safety/permission"
)

// The folder is blamed only for a call trusting it would let through: an ask
// rule or a class of call that needs a person is refused the same afterwards.
func TestFolderRefusalIsPerCall(t *testing.T) {
	folder := &folderRefusal{root: "/work/it's"}
	write := json.RawMessage(`{"path":"a.txt","content":"x"}`)
	cases := []struct {
		name     string
		policy   permission.Policy
		tool     string
		args     json.RawMessage
		wantCode string
		wantText string
	}{
		{"plain write", permission.New("ask", nil, nil, nil), "write_file", write, permission.RefusalUntrustedFolder, "reasonix trust --dir '/work/it'\\''s'"},
		{"ask rule", permission.New("ask", nil, []string{"write_file"}, nil), "write_file", write, permission.RefusalUnattended, "no interactive approver"},
		{"write scope escape", permission.New("ask", nil, nil, nil), "extend_write_paths", json.RawMessage(`{"path":"/x"}`), permission.RefusalUnattended, "no interactive approver"},
		{"high risk install plan", permission.New("ask", nil, nil, nil), "install_source", json.RawMessage(`{"planId":"high:abc","apply":true}`), permission.RefusalUnattended, "no interactive approver"},
		{"needs a person", permission.New("ask", nil, nil, nil), "computer_act", json.RawMessage(`{"app":"Notes","action":"click"}`), permission.RefusalUnattended, "approve it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := buildHeadlessGate(tc.policy, ToolApprovalAsk, folder)
			v, err := gate.Verdict(context.Background(), tc.tool, tc.args, false)
			if err != nil || v.Allow || v.Code != tc.wantCode || !strings.Contains(v.Reason, tc.wantText) {
				t.Fatalf("verdict %+v (err %v), want code %q mentioning %q", v, err, tc.wantCode, tc.wantText)
			}
			if strings.Contains(v.Reason, "--yes") && !strings.Contains(v.Reason, "only after") {
				t.Fatalf("the remedy offers the review-skipping form: %q", v.Reason)
			}
		})
	}
}

func TestDeclinedFolderIsNotUrged(t *testing.T) {
	gate := permission.NewGate(permission.New("ask", nil, nil, nil), denyPermissionApprover{folder: &folderRefusal{root: "/w", declined: true}})
	v, _ := gate.Verdict(context.Background(), "write_file", json.RawMessage(`{"path":"a","content":"x"}`), false)
	if !strings.Contains(v.Reason, "declined trust") || !strings.Contains(v.Reason, "changed their mind") {
		t.Fatalf("a declined folder must say so: %q", v.Reason)
	}
}
