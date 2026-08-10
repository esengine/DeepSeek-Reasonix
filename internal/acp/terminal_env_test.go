package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTerminalCreateSerializesEnvOverrides(t *testing.T) {
	req := newScriptedRequester()
	req.results["terminal/create"] = TerminalCreateResult{TerminalID: "term-env"}
	req.results["terminal/wait_for_exit"] = TerminalWaitResult{}
	exitZero := 0
	req.results["terminal/output"] = TerminalOutputResult{
		Output:     "ok",
		ExitStatus: &TerminalExitStatus{ExitCode: &exitZero},
	}
	req.results["terminal/release"] = struct{}{}
	io := newClientIO(req, "sess-env", ClientCapabilities{Terminal: true})

	env := map[string]string{
		"TMPDIR": "/private/session-tmp",
		"TMP":    "/private/session-tmp",
		"TEMP":   "/private/session-tmp",
	}
	if _, ok, err := io.RunCommand(context.Background(), "echo hi", "/proj", time.Minute, env); !ok || err != nil {
		t.Fatalf("RunCommand = ok=%v err=%v", ok, err)
	}

	raw, ok := req.params["terminal/create"]
	if !ok {
		t.Fatal("terminal/create params not recorded")
	}
	var params TerminalCreateParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Env) != 3 {
		t.Fatalf("env entries = %d, want 3: %+v", len(params.Env), params.Env)
	}
	got := map[string]string{}
	for _, e := range params.Env {
		got[e.Name] = e.Value
	}
	for k, v := range env {
		if got[k] != v {
			t.Fatalf("env[%s] = %q, want %q", k, got[k], v)
		}
	}
}

func TestEnvMapToVariablesStableOrder(t *testing.T) {
	got := envMapToVariables(map[string]string{"TMP": "a", "TEMP": "a", "TMPDIR": "a"})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	// TEMP < TMP < TMPDIR lexicographically.
	if got[0].Name != "TEMP" || got[1].Name != "TMP" || got[2].Name != "TMPDIR" {
		t.Fatalf("order = %v", got)
	}
	if envMapToVariables(nil) != nil {
		t.Fatal("nil map should yield nil")
	}
}
