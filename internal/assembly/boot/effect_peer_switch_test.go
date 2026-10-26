package boot

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"reasonix/internal/session/control"
)

func (p *peerToolProvider) toolsOfRequest(i int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var names []string
	for _, t := range p.reqs[i].Tools {
		names = append(names, t.Name)
	}
	return names
}

func (p *peerToolProvider) toolBytes(i int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, _ := json.Marshal(p.reqs[i].Tools)
	return string(raw)
}

func runTurn(t *testing.T, c interface {
	Run(context.Context, string) error
}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := c.Run(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
}

// The switch in settings reaches the provider request at the next turn and no
// earlier: a session that never turns it on sends the same tool bytes every
// turn, turning it on adds exactly the peer tools, and turning it off restores
// the original block.
func TestEffectPeerSwitchMovesTheToolBlockOnlyAtTurnBoundaries(t *testing.T) {
	home := statedBootHome(t)
	t.Setenv("REASONIX_HOME", home)
	rec, ctrl, _, _ := buildPeerToolSession(t, false, "nobody", "hello")
	runTurn(t, ctrl)
	runTurn(t, ctrl)
	if rec.toolBytes(0) != rec.toolBytes(1) {
		t.Fatal("a session that never touched the switch changed its tool block between turns")
	}
	base := rec.toolsOfRequest(0)

	yes := true
	if _, err := ctrl.SavePeersSettings(control.PeersPatch{Enabled: &yes, Dispatch: &yes}); err != nil {
		t.Fatal(err)
	}
	runTurn(t, ctrl)
	got := rec.toolsOfRequest(2)
	want := map[string]bool{"list_peers": true, "send_peer_message": true, "dispatch_to_project": true, "open_projects": true}
	for _, n := range got {
		delete(want, n)
	}
	if len(want) != 0 || len(got) != len(base)+4 {
		t.Fatalf("after turning on: %d tools (was %d), still missing %v", len(got), len(base), want)
	}

	no := false
	if _, err := ctrl.SavePeersSettings(control.PeersPatch{Enabled: &no, Dispatch: &no}); err != nil {
		t.Fatal(err)
	}
	runTurn(t, ctrl)
	if !reflect.DeepEqual(rec.toolsOfRequest(3), base) || rec.toolBytes(3) != rec.toolBytes(0) {
		t.Fatalf("after turning off the tool block is not what it was:\n%v\n%v", rec.toolsOfRequest(3), base)
	}
}
