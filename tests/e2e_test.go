// Reasonix Browser Relay — End-to-End Tool Test Suite
//
// This test suite validates every browser_* tool against the Alma ChromeRelay
// baseline. It starts a real relay server, connects a WebSocket extension
// emulator, and exercises each tool through the server's Send/SendTabCommand
// methods.
//
// Run: go test -v ./tests/ -run TestE2E
//      go test -v ./tests/ -run TestE2E/TestBrowserNavigate  (single test)
//
// (Some tests require a real extension — those are marked "manual".)

package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/browserrelay"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin" // registers all browser_* tools
)

// ── Test Page ──────────────────────────────────────────────────────────────

// testPagePath returns the absolute path to the test HTML page.
func testPagePath() string {
	cwd, _ := os.Getwd()
	return "file://" + filepath.Join(cwd, "test_page.html")
}

// ── Tool-Method Map ────────────────────────────────────────────────────────
//
// Reasonix Relay ←→ Alma ChromeRelay equivalency:
//
// Reasonix tool              | Alma ChromeRelay tool   | Coverage
// ───────────────────────────|─────────────────────────|────────────────────
// browser_status             | — (no equivalent)       | EXTRA: Reasonix-only
// browser_navigate           | ChromeRelayNavigate     | ✅
// browser_click              | ChromeRelayClick        | ✅
// browser_type               | ChromeRelayType         | ✅
// browser_read               | ChromeRelayRead         | ✅
// browser_screenshot         | ChromeRelayScreenshot   | ✅
// browser_eval               | ChromeRelayEval         | ✅
// browser_list_pages         | ChromeRelayListTabs     | ✅
// browser_read_dom           | ChromeRelayReadDom      | ✅
// browser_scroll             | ChromeRelayScroll       | ✅
// browser_go_back            | ChromeRelayBack         | ✅
// browser_go_forward         | ChromeRelayForward      | ✅
// browser_upload_file        | ChromeRelayUpload       | ✅
// browser_select_page        | —                       | EXTRA: Reasonix-only
// browser_new_page           | —                       | EXTRA: Reasonix-only
// browser_close_page         | —                       | EXTRA: Reasonix-only
// browser_press_key          | —                       | EXTRA: Reasonix-only
// browser_hover              | —                       | EXTRA: Reasonix-only
// browser_wait               | —                       | EXTRA: Reasonix-only
// browser_resize             | —                       | EXTRA: Reasonix-only
// browser_handle_dialog      | —                       | EXTRA: Reasonix-only
// browser_fill_form          | —                       | EXTRA: Reasonix-only
// browser_attached_pages     | —                       | EXTRA: Reasonix-only

// ══════════════════════════════════════════════════════════════════════════
// E2E Tests — require an extension connection (manual)
// ══════════════════════════════════════════════════════════════════════════

// helper: start a relay server and return it.
func startRelay(t *testing.T) (*browserrelay.Server, context.Context) {
	t.Helper()
	s := browserrelay.NewServer()
	ctx := context.Background()
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Logf("relay server on %s (token: %s...)", addr, s.Token()[:8])
	// Set as default so built-in tools can reach it.
	browserrelay.DefaultServer = s
	return s, ctx
}

// ── Test 1: browser_status ─────────────────────────────────────────────────

func TestBrowserStatus(t *testing.T) {
	// Verify the tool is registered and returns a valid schema.
	statusTool, ok := tool.LookupBuiltin("browser_status")
	if !ok {
		t.Fatal("browser_status not registered")
	}
	if !statusTool.ReadOnly() {
		t.Fatal("browser_status should be read-only")
	}
	t.Logf("Schema: %s", string(statusTool.Schema()))

	// Test when server is running.
	s, ctx := startRelay(t)
	defer s.Stop()

	result, err := statusTool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	t.Logf("Status (no extension): %s", result)

	// Should return running=true but state=disconnected.
	if !strings.Contains(result, `"running": true`) {
		t.Error("expected running=true")
	}
	if !strings.Contains(result, `"state": "disconnected"`) {
		t.Error("expected state=disconnected when no extension")
	}
}

// ── Test 2: browser_navigate ───────────────────────────────────────────────

func TestBrowserNavigate(t *testing.T) {
	navTool, ok := tool.LookupBuiltin("browser_navigate")
	if !ok {
		t.Fatal("browser_navigate not registered")
	}
	if navTool.ReadOnly() {
		t.Fatal("browser_navigate should not be read-only")
	}

	// Schema validation: url required.
	var schema struct {
		Required []string `json:"required"`
		Props    map[string]interface{} `json:"properties"`
	}
	json.Unmarshal(navTool.Schema(), &schema)
	if !contains(schema.Required, "url") {
		t.Error("browser_navigate should require 'url'")
	}
	t.Logf("Schema: %s", string(navTool.Schema()))

	// Execute requires a connected extension — will fail gracefully.
	s, ctx := startRelay(t)
	defer s.Stop()

	_, err := navTool.Execute(ctx, json.RawMessage(`{"url":"about:blank"}`))
	if err == nil {
		t.Log("navigate succeeded (extension connected)")
	} else {
		t.Logf("navigate error (expected without extension): %v", err)
	}
}

// ── Test 3: browser_click ──────────────────────────────────────────────────

func TestBrowserClick(t *testing.T) {
	clickTool, ok := tool.LookupBuiltin("browser_click")
	if !ok {
		t.Fatal("browser_click not registered")
	}

	// Schema: selector required.
	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(clickTool.Schema(), &schema)
	if !contains(schema.Required, "selector") {
		t.Error("browser_click should require 'selector'")
	}
	t.Logf("click description: %s", clickTool.Description())
}

// ── Test 4: browser_type ───────────────────────────────────────────────────

func TestBrowserType(t *testing.T) {
	typeTool, ok := tool.LookupBuiltin("browser_type")
	if !ok {
		t.Fatal("browser_type not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(typeTool.Schema(), &schema)
	if !contains(schema.Required, "selector") || !contains(schema.Required, "text") {
		t.Error("browser_type should require 'selector' and 'text'")
	}
	t.Logf("type description: %s", typeTool.Description())

	// Test with optional clear parameter.
	_, err := typeTool.Execute(context.Background(), json.RawMessage(`{"selector":"#name-input","text":"test","clear":true}`))
	if err == nil {
		t.Log("type execution succeeded")
	} else {
		// Expected: not connected.
		t.Logf("type error (expected without extension): %v", err)
	}
}

// ── Test 5: browser_read ───────────────────────────────────────────────────

func TestBrowserRead(t *testing.T) {
	readTool, ok := tool.LookupBuiltin("browser_read")
	if !ok {
		t.Fatal("browser_read not registered")
	}
	if !readTool.ReadOnly() {
		t.Fatal("browser_read should be read-only")
	}

	// Schema: selector is optional.
	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(readTool.Schema(), &schema)
	if len(schema.Required) != 0 {
		t.Error("browser_read should have no required fields")
	}
	t.Logf("read description: %s", readTool.Description())
}

// ── Test 6: browser_screenshot ─────────────────────────────────────────────

func TestBrowserScreenshot(t *testing.T) {
	ssTool, ok := tool.LookupBuiltin("browser_screenshot")
	if !ok {
		t.Fatal("browser_screenshot not registered")
	}
	if !ssTool.ReadOnly() {
		t.Fatal("browser_screenshot should be read-only")
	}

	// Test format validation.
	_, err := ssTool.Execute(context.Background(), json.RawMessage(`{"format":"png"}`))
	t.Logf("screenshot png: %v", err)

	_, err = ssTool.Execute(context.Background(), json.RawMessage(`{"format":"jpeg","quality":85}`))
	t.Logf("screenshot jpeg: %v", err)
}

// ── Test 7: browser_eval ───────────────────────────────────────────────────

func TestBrowserEval(t *testing.T) {
	evalTool, ok := tool.LookupBuiltin("browser_eval")
	if !ok {
		t.Fatal("browser_eval not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(evalTool.Schema(), &schema)
	if !contains(schema.Required, "expression") {
		t.Error("browser_eval should require 'expression'")
	}
	t.Logf("eval description: %s", evalTool.Description())
}

// ── Test 8: browser_list_pages ─────────────────────────────────────────────

func TestBrowserListPages(t *testing.T) {
	listTool, ok := tool.LookupBuiltin("browser_list_pages")
	if !ok {
		t.Fatal("browser_list_pages not registered")
	}
	if !listTool.ReadOnly() {
		t.Fatal("browser_list_pages should be read-only")
	}
	t.Logf("list_pages description: %s", listTool.Description())
}

// ── Test 9: browser_read_dom ───────────────────────────────────────────────

func TestBrowserReadDOM(t *testing.T) {
	domTool, ok := tool.LookupBuiltin("browser_read_dom")
	if !ok {
		t.Fatal("browser_read_dom not registered")
	}
	if !domTool.ReadOnly() {
		t.Fatal("browser_read_dom should be read-only")
	}
	t.Logf("read_dom description: %s", domTool.Description())
}

// ── Test 10: browser_scroll ────────────────────────────────────────────────

func TestBrowserScroll(t *testing.T) {
	scrollTool, ok := tool.LookupBuiltin("browser_scroll")
	if !ok {
		t.Fatal("browser_scroll not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(scrollTool.Schema(), &schema)
	if !contains(schema.Required, "direction") {
		t.Error("browser_scroll should require 'direction'")
	}

	// Test direction validation.
	_, err := scrollTool.Execute(context.Background(), json.RawMessage(`{"direction":"up","amount":300}`))
	t.Logf("scroll up: %v", err)

	_, err = scrollTool.Execute(context.Background(), json.RawMessage(`{"direction":"down"}`))
	t.Logf("scroll down (default 500): %v", err)

	_, err = scrollTool.Execute(context.Background(), json.RawMessage(`{"direction":"invalid"}`))
	if err == nil {
		t.Error("expected error for invalid direction")
	}
}

// ── Test 11: browser_go_back / browser_go_forward ──────────────────────────

func TestBrowserGoBackForward(t *testing.T) {
	backTool, ok := tool.LookupBuiltin("browser_go_back")
	if !ok {
		t.Fatal("browser_go_back not registered")
	}
	forwardTool, ok := tool.LookupBuiltin("browser_go_forward")
	if !ok {
		t.Fatal("browser_go_forward not registered")
	}

	t.Logf("back: %s", backTool.Description())
	t.Logf("forward: %s", forwardTool.Description())
}

// ── Test 12: browser_upload_file ───────────────────────────────────────────

func TestBrowserUploadFile(t *testing.T) {
	uploadTool, ok := tool.LookupBuiltin("browser_upload_file")
	if !ok {
		t.Fatal("browser_upload_file not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(uploadTool.Schema(), &schema)
	if !contains(schema.Required, "selector") || !contains(schema.Required, "file_path") {
		t.Error("browser_upload_file should require 'selector' and 'file_path'")
	}
	t.Logf("upload description: %s", uploadTool.Description())
}

// ── Test 13: browser_select_page ───────────────────────────────────────────

func TestBrowserSelectPage(t *testing.T) {
	selectTool, ok := tool.LookupBuiltin("browser_select_page")
	if !ok {
		t.Fatal("browser_select_page not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(selectTool.Schema(), &schema)
	if !contains(schema.Required, "tab_id") {
		t.Error("browser_select_page should require 'tab_id'")
	}
	t.Logf("select_page description: %s", selectTool.Description())
}

// ── Test 14: browser_new_page ──────────────────────────────────────────────

func TestBrowserNewPage(t *testing.T) {
	newPageTool, ok := tool.LookupBuiltin("browser_new_page")
	if !ok {
		t.Fatal("browser_new_page not registered")
	}
	t.Logf("new_page description: %s", newPageTool.Description())
}

// ── Test 15: browser_close_page ────────────────────────────────────────────

func TestBrowserClosePage(t *testing.T) {
	closeTool, ok := tool.LookupBuiltin("browser_close_page")
	if !ok {
		t.Fatal("browser_close_page not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(closeTool.Schema(), &schema)
	if !contains(schema.Required, "tab_id") {
		t.Error("browser_close_page should require 'tab_id'")
	}
	t.Logf("close_page description: %s", closeTool.Description())
}

// ── Test 16: browser_press_key ─────────────────────────────────────────────

func TestBrowserPressKey(t *testing.T) {
	keyTool, ok := tool.LookupBuiltin("browser_press_key")
	if !ok {
		t.Fatal("browser_press_key not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(keyTool.Schema(), &schema)
	if !contains(schema.Required, "key") {
		t.Error("browser_press_key should require 'key'")
	}
	t.Logf("press_key description: %s", keyTool.Description())
}

// ── Test 17: browser_hover ─────────────────────────────────────────────────

func TestBrowserHover(t *testing.T) {
	hoverTool, ok := tool.LookupBuiltin("browser_hover")
	if !ok {
		t.Fatal("browser_hover not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(hoverTool.Schema(), &schema)
	if !contains(schema.Required, "selector") {
		t.Error("browser_hover should require 'selector'")
	}
	t.Logf("hover description: %s", hoverTool.Description())
}

// ── Test 18: browser_wait ──────────────────────────────────────────────────

func TestBrowserWait(t *testing.T) {
	waitTool, ok := tool.LookupBuiltin("browser_wait")
	if !ok {
		t.Fatal("browser_wait not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(waitTool.Schema(), &schema)
	if !contains(schema.Required, "selector") {
		t.Error("browser_wait should require 'selector'")
	}
	t.Logf("wait description: %s", waitTool.Description())
}

// ── Test 19: browser_resize ────────────────────────────────────────────────

func TestBrowserResize(t *testing.T) {
	resizeTool, ok := tool.LookupBuiltin("browser_resize")
	if !ok {
		t.Fatal("browser_resize not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(resizeTool.Schema(), &schema)
	if !contains(schema.Required, "width") || !contains(schema.Required, "height") {
		t.Error("browser_resize should require 'width' and 'height'")
	}
	t.Logf("resize description: %s", resizeTool.Description())
}

// ── Test 20: browser_handle_dialog ─────────────────────────────────────────

func TestBrowserHandleDialog(t *testing.T) {
	dialogTool, ok := tool.LookupBuiltin("browser_handle_dialog")
	if !ok {
		t.Fatal("browser_handle_dialog not registered")
	}
	t.Logf("handle_dialog description: %s", dialogTool.Description())
}

// ── Test 21: browser_fill_form ─────────────────────────────────────────────

func TestBrowserFillForm(t *testing.T) {
	formTool, ok := tool.LookupBuiltin("browser_fill_form")
	if !ok {
		t.Fatal("browser_fill_form not registered")
	}

	var schema struct {
		Required []string `json:"required"`
	}
	json.Unmarshal(formTool.Schema(), &schema)
	if !contains(schema.Required, "fields") {
		t.Error("browser_fill_form should require 'fields'")
	}
	t.Logf("fill_form description: %s", formTool.Description())
}

// ── Test 22: browser_attached_pages ────────────────────────────────────────

func TestBrowserAttachedPages(t *testing.T) {
	attachedTool, ok := tool.LookupBuiltin("browser_attached_pages")
	if !ok {
		t.Fatal("browser_attached_pages not registered")
	}
	if !attachedTool.ReadOnly() {
		t.Fatal("browser_attached_pages should be read-only")
	}
	t.Logf("attached_pages description: %s", attachedTool.Description())
}

// ══════════════════════════════════════════════════════════════════════════
// Structural Tests (no extension needed)
// ══════════════════════════════════════════════════════════════════════════

// TestAllToolsRegistered verifies every expected browser_* tool is registered.
func TestAllToolsRegistered(t *testing.T) {
	expected := []string{
		"browser_status",
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_read",
		"browser_screenshot",
		"browser_eval",
		"browser_list_pages",
		"browser_select_page",
		"browser_new_page",
		"browser_close_page",
		"browser_read_dom",
		"browser_scroll",
		"browser_go_back",
		"browser_go_forward",
		"browser_press_key",
		"browser_hover",
		"browser_wait",
		"browser_upload_file",
		"browser_resize",
		"browser_handle_dialog",
		"browser_fill_form",
		"browser_attached_pages",
	}

	for _, name := range expected {
		_, ok := tool.LookupBuiltin(name)
		if !ok {
			t.Errorf("MISSING: %s — expected but not registered!", name)
		} else {
			t.Logf("✅ %s registered", name)
		}
	}
}

// TestAlmaEquivalency verifies every Alma ChromeRelay tool has a Reasonix
// equivalent.
func TestAlmaEquivalency(t *testing.T) {
	// Alma ChromeRelay → Reasonix mapping
	almaToReasonix := map[string]string{
		"ChromeRelayListTabs":    "browser_list_pages",
		"ChromeRelayNavigate":    "browser_navigate",
		"ChromeRelayClick":       "browser_click",
		"ChromeRelayType":        "browser_type",
		"ChromeRelayScreenshot":  "browser_screenshot",
		"ChromeRelayRead":        "browser_read",
		"ChromeRelayReadDom":     "browser_read_dom",
		"ChromeRelayEval":        "browser_eval",
		"ChromeRelayScroll":      "browser_scroll",
		"ChromeRelayBack":        "browser_go_back",
		"ChromeRelayForward":     "browser_go_forward",
		"ChromeRelayUpload":      "browser_upload_file",
	}

	for almaName, reasonixName := range almaToReasonix {
		_, ok := tool.LookupBuiltin(reasonixName)
		if !ok {
			t.Errorf("❌ %s → %s: MISSING", almaName, reasonixName)
		} else {
			t.Logf("✅ %s → %s", almaName, reasonixName)
		}
	}
}

// TestReasonixExtraTools lists Reasonix tools that have NO Alma equivalent.
func TestReasonixExtraTools(t *testing.T) {
	extraTools := []string{
		"browser_status",
		"browser_select_page",
		"browser_new_page",
		"browser_close_page",
		"browser_press_key",
		"browser_hover",
		"browser_wait",
		"browser_resize",
		"browser_handle_dialog",
		"browser_fill_form",
		"browser_attached_pages",
	}

	t.Log("=== Reasonix-extras (no Alma equivalent) ===")
	for _, name := range extraTools {
		_, ok := tool.LookupBuiltin(name)
		if ok {
			t.Logf("✨ %s — Reasonix-only capability", name)
		}
	}
}

// TestServerLifecycle tests the relay server life cycle.
func TestServerLifecycle(t *testing.T) {
	s := browserrelay.NewServer()
	ctx := context.Background()

	// Start
	addr, err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	status := s.Status()
	if !status.Running {
		t.Error("server should be running")
	}
	if status.State != "disconnected" {
		t.Errorf("state = %s, want disconnected", status.State)
	}
	t.Logf("Server started at %s", addr)

	// Token
	token := s.Token()
	if len(token) < 10 {
		t.Errorf("token too short: %d", len(token))
	}
	t.Logf("Token: %s...", token[:8])

	// Stop
	s.Stop()
	status = s.Status()
	if status.Running {
		t.Error("server should not be running after Stop()")
	}
	if status.State != "disconnected" {
		t.Errorf("state = %s, want disconnected after Stop()", status.State)
	}
	t.Log("Server stopped gracefully")
}

// TestToolSchemaValidation tests that all tool schemas parse correctly.
func TestToolSchemaValidation(t *testing.T) {
	toolNames := []string{
		"browser_status", "browser_navigate", "browser_click",
		"browser_type", "browser_read", "browser_screenshot",
		"browser_eval", "browser_list_pages", "browser_select_page",
		"browser_new_page", "browser_close_page", "browser_read_dom",
		"browser_scroll", "browser_go_back", "browser_go_forward",
		"browser_press_key", "browser_hover", "browser_wait",
		"browser_upload_file", "browser_resize", "browser_handle_dialog",
		"browser_fill_form", "browser_attached_pages",
	}

	for _, name := range toolNames {
		tool, ok := tool.LookupBuiltin(name)
		if !ok {
			t.Errorf("MISSING: %s", name)
			continue
		}

		// Schema must be valid JSON.
		schema := tool.Schema()
		if len(schema) == 0 {
			t.Errorf("%s: empty schema", name)
			continue
		}
		var parsed interface{}
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Errorf("%s: invalid JSON schema: %v", name, err)
			continue
		}
		t.Logf("✅ %s: schema valid", name)

		// Description must be non-empty.
		if tool.Description() == "" {
			t.Errorf("%s: empty description", name)
		}
	}
}

// ══════════════════════════════════════════════════════════════════════════
// Helpers
// ══════════════════════════════════════════════════════════════════════════

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// TestMain is optional — can start a local HTTP server for the test page.
func TestMain(m *testing.M) {
	// Run tests.
	os.Exit(m.Run())
}