package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestStdioEndToEnd drives a real subprocess (this test binary re-invoked in
// helper mode) through the full MCP handshake and a tool call, exercising
// StartAll, tools/list, and tools/call over stdio JSON-RPC.
func TestStdioEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec := Spec{
		Name:    "mock",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}

	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()

	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
	if got := tools[0].Name(); got != "mcp__mock__echo" {
		t.Fatalf("tool name: want mcp__mock__echo, got %q", got)
	}
	if got, want := string(tools[0].Schema()), `{"properties":{"msg":{"type":"string"}},"required":["msg","z"],"type":"object"}`; got != want {
		t.Fatalf("tool schema = %s, want %s", got, want)
	}

	out, err := tools[0].Execute(ctx, json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "echo: hi" {
		t.Fatalf("result: want %q, got %q", "echo: hi", out)
	}
}

func TestStartAvailableKeepsGoodServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	good := Spec{
		Name:    "good",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	bad := Spec{Name: "bad", Command: "reasonix-missing-mcp-binary"}

	host, tools := StartAvailable(ctx, []Spec{bad, good})
	defer host.Close()

	if len(tools) != 2 {
		t.Fatalf("want tools from the good server, got %d", len(tools))
	}
	if got := host.ServerNames(); len(got) != 1 || got[0] != "good" {
		t.Fatalf("connected servers = %v, want [good]", got)
	}
	failures := host.Failures()
	if len(failures) != 1 || failures[0].Name != "bad" {
		t.Fatalf("failures = %+v, want bad", failures)
	}
}

// TestStartAllAllOrNothingOnFailure pins the strict StartAll contract the
// parallel rewrite must preserve: any single plugin failing aborts the whole
// set, returns no Host or tools, and tears down every server that did start —
// including, under parallel start, a good server whose index sits after the
// failing one ([bad, good]). On error the Host is nil, so callers never see a
// half-built set; the started servers are closed before StartAll returns.
func TestStartAllAllOrNothingOnFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	good := Spec{
		Name:    "good",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}
	bad := Spec{Name: "bad", Command: "reasonix-missing-mcp-binary"}

	for _, tc := range []struct {
		name  string
		specs []Spec
	}{
		{"failure first", []Spec{bad, good}},
		{"failure last", []Spec{good, bad}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, tools, err := StartAll(ctx, tc.specs)
			if err == nil {
				if host != nil {
					host.Close()
				}
				t.Fatal("StartAll should fail when a plugin can't start")
			}
			if host != nil || tools != nil {
				t.Fatalf("failed StartAll must return nil host/tools, got host=%v tools=%d", host, len(tools))
			}
		})
	}
}

func TestStdioFailureCapturesStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, _ := StartAvailable(ctx, []Spec{{
		Name:    "stderr",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_HELPER_STDERR_EXIT": "1"},
	}})
	defer host.Close()

	failures := host.Failures()
	if len(failures) != 1 {
		t.Fatalf("failures = %+v, want one", failures)
	}
	if !strings.Contains(failures[0].Error, "helper stderr boom") {
		t.Fatalf("failure should include stderr, got %q", failures[0].Error)
	}
}

// TestHelperProcess is not a real test; it acts as a minimal MCP stdio server
// when invoked by TestStdioEndToEnd. It exits before the test framework can
// print to stdout, keeping the JSON-RPC channel clean.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_STDERR_EXIT") == "1" {
		os.Stderr.WriteString("helper stderr boom\n")
		os.Exit(2)
	}
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification: no response
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "mock", "version": "0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "zed",
				"description": "Sorted after echo.",
				"inputSchema": map[string]any{"type": "object"},
			}, {
				"name":        "echo",
				"description": "Echo back the message.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"msg": map[string]any{"type": "string"}},
					"required":   []string{"z", "msg"},
				},
			}}}
		case "tools/call":
			var p struct {
				Arguments struct {
					Msg string `json:"msg"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			result = map[string]any{"content": []map[string]any{
				{"type": "text", "text": "echo: " + p.Arguments.Msg},
			}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		os.Stdout.Write(append(b, '\n'))
	}
}
