package control

import (
	"testing"

	"reasonix/internal/permission"
)

func TestGrantSessionSkipsUnsafeBashCommand(t *testing.T) {
	approval := newApprovalManager(permission.New("ask", nil, nil, nil), ToolApprovalAsk, 0)
	approval.grantSession("bash", `echo $(touch /tmp/reasonix-permission-bypass)`)
	if len(approval.granted) != 0 {
		t.Fatalf("unsafe bash command created session grants: %+v", approval.granted)
	}
}
