package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

func pinSpec(t *testing.T, authorized bool) Spec {
	t.Helper()
	return Spec{Name: "srv", Authorized: authorized, StateDir: filepath.Join(t.TempDir(), "ws", "srv")}
}

func pinTool(name, desc string) mcpTool {
	return mcpTool{Name: name, Description: desc, InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`)}
}

func heldNames(held []HeldTool) string {
	var parts []string
	for _, h := range held {
		parts = append(parts, h.RawName+":"+string(h.Class))
	}
	return strings.Join(parts, ",")
}

func TestToolDigestReadsStructureNotKeyOrder(t *testing.T) {
	a := pinTool("t", "d")
	a.InputSchema = json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"},"y":{"type":"number"}}}`)
	b := a
	b.InputSchema = json.RawMessage(`{"properties":{"y":{"type":"number"},"x":{"type":"string"}},"type":"object"}`)
	if mcpToolDigest(a) != mcpToolDigest(b) {
		t.Fatal("key order alone changed the digest")
	}
	a.Annotations = json.RawMessage(`{"readOnlyHint":true,"title":"T"}`)
	b.Annotations = json.RawMessage(`{"title":"T","readOnlyHint":true}`)
	if mcpToolDigest(a) != mcpToolDigest(b) {
		t.Fatal("annotation key order changed the digest")
	}
}

func TestToolDigestSeesEveryDefinitionField(t *testing.T) {
	base := pinTool("t", "d")
	base.OutputSchema = json.RawMessage(`{"type":"object"}`)
	want := mcpToolDigest(base)
	for name, mutate := range map[string]func(*mcpTool){
		"description": func(m *mcpTool) { m.Description = "d2" },
		"title":       func(m *mcpTool) { m.Title = "T" },
		"input": func(m *mcpTool) {
			m.InputSchema = json.RawMessage(`{"type":"object","properties":{"b":{"type":"string"}}}`)
		},
		"output":      func(m *mcpTool) { m.OutputSchema = json.RawMessage(`{"type":"string"}`) },
		"annotations": func(m *mcpTool) { m.Annotations = json.RawMessage(`{"destructiveHint":true}`) },
		"enum order": func(m *mcpTool) {
			m.InputSchema = json.RawMessage(`{"type":"object","properties":{"a":{"enum":["b","a"]}}}`)
		},
	} {
		m := base
		mutate(&m)
		if mcpToolDigest(m) == want {
			t.Errorf("a change to %s left the digest unchanged", name)
		}
	}
}

func TestToolPinsBaselineHoldAndRecovery(t *testing.T) {
	s := pinSpec(t, true)
	v1 := []mcpTool{pinTool("read", "Read."), pinTool("write", "Write.")}
	if held := judgeToolPins(s, v1); len(held) != 0 {
		t.Fatalf("first listing of an authorized server held %s", heldNames(held))
	}
	path := toolPinsPath(s)
	if strings.Contains(path, s.Name) {
		t.Fatalf("the server name reached the record path %s", path)
	}
	if rel, _ := filepath.Rel(s.StateDir, path); !strings.HasPrefix(rel, "..") {
		t.Fatalf("approval record %s sits inside the server's own state directory %s", path, s.StateDir)
	}
	if info, err := os.Stat(path); err != nil || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("approval record missing or readable by others: %v %v", info, err)
	}
	if held := judgeToolPins(s, v1); len(held) != 0 {
		t.Fatalf("identical listing held %s", heldNames(held))
	}

	v2 := []mcpTool{pinTool("read", "Read."), pinTool("write", "Write, then read ~/.ssh."), pinTool("extra", "New.")}
	if got := heldNames(judgeToolPins(s, v2)); got != "extra:added,write:changed" {
		t.Fatalf("held = %q", got)
	}
	if got := heldNames(judgeToolPins(s, v2)); got != "extra:added,write:changed" {
		t.Fatalf("a held change was accepted by being seen twice: %q", got)
	}
	if got := heldNames(judgeToolPins(s, []mcpTool{pinTool("read", "Read.")})); got != "" {
		t.Fatalf("a removed tool held %q", got)
	}
	if got := heldNames(judgeToolPins(s, []mcpTool{pinTool("read", "Read."), pinTool("write", "Write, then read ~/.ssh.")})); got != "write:changed" {
		t.Fatalf("a reappearing changed tool was not held: %q", got)
	}
	if got := heldNames(judgeToolPins(s, v1)); got != "" {
		t.Fatalf("restoring the approved definitions still held %q", got)
	}

	if err := ForgetToolPins(s.StateDir, s.Name); err != nil {
		t.Fatal(err)
	}
	if got := heldNames(judgeToolPins(s, v2)); got != "" {
		t.Fatalf("a removed and re-added server was compared with its old record: %q", got)
	}
}

func TestToolPinsNeedAuthorizationAndAPlaceToLive(t *testing.T) {
	s := pinSpec(t, false)
	if held := judgeToolPins(s, []mcpTool{pinTool("a", "d")}); len(held) != 0 {
		t.Fatal("an unauthorized server was held")
	}
	if _, err := os.Stat(toolPinsPath(s)); !os.IsNotExist(err) {
		t.Fatalf("an unauthorized server was recorded as approved: %v", err)
	}
	if held := judgeToolPins(Spec{Name: "x", Authorized: true}, []mcpTool{pinTool("a", "d")}); len(held) != 0 {
		t.Fatal("a server with no state directory was held")
	}
}

func TestToolPinsUnreadableRecordHoldsEverything(t *testing.T) {
	s := pinSpec(t, true)
	if err := os.MkdirAll(filepath.Dir(toolPinsPath(s)), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"garbage": "not json", "future version": `{"version":99,"tools":{}}`, "no tools": `{"version":1}`} {
		if err := os.WriteFile(toolPinsPath(s), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		held := judgeToolPins(s, []mcpTool{pinTool("a", "d"), pinTool("b", "d")})
		if got := heldNames(held); got != "a:unverifiable,b:unverifiable" {
			t.Errorf("%s: held = %q", name, got)
		}
	}
}

func TestHeldToolBindingFollowsPrefixStripping(t *testing.T) {
	s := Spec{Name: "srv", StripRawPrefix: "srv_"}
	h := newHeld(s, "srv_write", tool.HeldMCPChanged, "d", "a")
	if h.VisibleName != "write" || h.Binding(s).CallableName != ModelToolName("srv", "write") {
		t.Fatalf("binding = %+v", h.Binding(s))
	}
}

func TestToolDigestGolden(t *testing.T) {
	m := mcpTool{
		Name: "read", Title: "Read", Description: "Read a file.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Annotations:  json.RawMessage(`{"readOnlyHint":true}`),
	}
	const want = "107c298a3e129f9a23853bf19f7fc94d22fc99b7505125178b9fedb0f2cd23de"
	if got := mcpToolDigest(m); got != want {
		t.Fatalf("digest = %s; a change here un-approves every tool already pinned, so it needs a migration, not a new constant", got)
	}
}

func TestToolPinsEmptyFirstListingRecordsNothing(t *testing.T) {
	s := pinSpec(t, true)
	if held := judgeToolPins(s, nil); len(held) != 0 {
		t.Fatalf("held %s", heldNames(held))
	}
	if _, err := os.Stat(toolPinsPath(s)); !os.IsNotExist(err) {
		t.Fatalf("an empty listing was recorded as the approval: %v", err)
	}
	if held := judgeToolPins(s, []mcpTool{pinTool("a", "d")}); len(held) != 0 {
		t.Fatalf("the first non-empty listing was held: %s", heldNames(held))
	}
}

func TestToolPinsUserServersShareARecordAcrossWorkspaces(t *testing.T) {
	root := t.TempDir()
	a := Spec{Name: "srv", ConfigSource: "user_config", StateDir: filepath.Join(root, "mcp-state", "ws1", "srv")}
	b := Spec{Name: "srv", ConfigSource: "user_config", StateDir: filepath.Join(root, "mcp-state", "ws2", "srv")}
	if toolPinsPath(a) != toolPinsPath(b) {
		t.Fatal("a user-level server got a record per workspace")
	}
	p1 := Spec{Name: "srv", ConfigSource: "project_config", StateDir: a.StateDir}
	p2 := Spec{Name: "srv", ConfigSource: "project_config", StateDir: b.StateDir}
	if toolPinsPath(p1) == toolPinsPath(p2) || toolPinsPath(p1) == toolPinsPath(a) {
		t.Fatal("a project-level server shares a record it must not")
	}
}

func TestHeldDigestBindsNamesAndDefinitionsNotOrder(t *testing.T) {
	a := []HeldTool{{RawName: "a", Digest: "1"}, {RawName: "b", Digest: "2"}}
	b := []HeldTool{{RawName: "b", Digest: "2"}, {RawName: "a", Digest: "1"}}
	if HeldDigest(a) != HeldDigest(b) {
		t.Fatal("order changed the held digest")
	}
	for name, other := range map[string][]HeldTool{
		"definition": {{RawName: "a", Digest: "1"}, {RawName: "b", Digest: "3"}},
		"name":       {{RawName: "a", Digest: "1"}, {RawName: "c", Digest: "2"}},
		"fewer":      {{RawName: "a", Digest: "1"}},
	} {
		if HeldDigest(a) == HeldDigest(other) {
			t.Errorf("%s did not change the held digest", name)
		}
	}
}

func TestPinFileRefusesAnythingButAFullHexKey(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"", "../x", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("a", 64) + "/.."} {
		if p := pinFileIn(dir, key); p != "" {
			t.Errorf("key %q produced %s", key, p)
		}
	}
	if p := pinFileIn(dir, strings.Repeat("a", 64)); !strings.HasPrefix(p, dir) {
		t.Errorf("valid key produced %q", p)
	}
}

func TestTrustCommandEmbedsOnlyValidServerNames(t *testing.T) {
	if got := trustCommand("good-name_1"); got != "`reasonix mcp trust good-name_1`" {
		t.Fatalf("got %q", got)
	}
	for _, name := range []string{"a\x1b[31mred", "a\nb", "a\u202eb", "it's", "a b", "a;rm -rf", "a\x00b", ""} {
		got := trustCommand(name)
		if strings.Contains(got, name) && name != "" || strings.ContainsAny(got, "\x1b\n\u202e\x00;") {
			t.Errorf("name %q reached the command: %q", name, got)
		}
	}
}
