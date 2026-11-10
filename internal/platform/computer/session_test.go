package computer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

var nativeDriver *Driver

func TestMain(m *testing.M) {
	if os.Getenv("REASONIX_CUA_FAKE") != "" {
		runFakeCUA(os.Stdin, os.Stdout)
		return
	}
	code := m.Run()
	if nativeDriver != nil {
		nativeDriver.Close()
	}
	os.Exit(code)
}

func fakeScreenshot() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4000, 2000)))
	return buf.Bytes()
}

func runFakeCUA(in io.Reader, out io.Writer) {
	sc := bufio.NewScanner(in)
	enc := json.NewEncoder(out)
	value := ""
	snapshot := 0
	launched := false
	for sc.Scan() {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.Unmarshal(sc.Bytes(), &req)
		if req.ID == 0 {
			continue
		}
		version := Contract().Version
		if v := os.Getenv("REASONIX_CUA_FAKE_VERSION"); v != "" {
			version = v
		}
		var result any = map[string]any{"serverInfo": map[string]any{"version": version}}
		if req.Method == "tools/list" {
			tools := []any{}
			for name := range driverTools {
				properties := map[string]any{}
				if name != os.Getenv("REASONIX_CUA_NO_DELIVERY_MODE") {
					properties["delivery_mode"] = map[string]any{"type": "string"}
				}
				tools = append(tools, map[string]any{"name": name, "inputSchema": map[string]any{"type": "object", "properties": properties}})
			}
			if !driverTools["launch_app"] {
				tools = append(tools, map[string]any{"name": "launch_app", "inputSchema": map[string]any{"type": "object"}})
			}
			result = map[string]any{"tools": tools}
		}
		if req.Method == "tools/call" {
			if req.Params.Name == "list_apps" && os.Getenv("REASONIX_CUA_OVERSIZE") != "" {
				_, _ = io.WriteString(out, strings.Repeat("x", 33<<20)+"\n")
				continue
			}
			if req.Params.Name == os.Getenv("REASONIX_CUA_SILENT") {
				continue
			}
			if path := os.Getenv("REASONIX_CUA_REQUEST_LOG"); path != "" {
				f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
				if f != nil {
					_ = json.NewEncoder(f).Encode(req.Params)
					_ = f.Close()
				}
			}
			args := req.Params.Arguments
			pid := os.Getpid()
			content := []any{}
			structured := map[string]any{"effect": "confirmed", "route": "accessibility", "summary": "fixture readback", "evidence": []any{map[string]any{"kind": "value_readback"}}}
			switch req.Params.Name {
			case "list_apps":
				apps := []any{map[string]any{"pid": pid, "bundle_id": "com.example.Notes", "name": "Notes", "running": true, "active": true}}
				extra, _ := strconv.Atoi(os.Getenv("REASONIX_CUA_EXTRA_APPS"))
				for i := range extra {
					apps = append(apps, map[string]any{"pid": pid, "bundle_id": fmt.Sprintf("com.example.Other%d", i), "running": true})
				}
				structured = map[string]any{"apps": apps}
				if path := os.Getenv("REASONIX_CUA_INSTALLED_APP"); path != "" {
					app := map[string]any{"pid": 0, "bundle_id": path, "name": "Notes", "running": false, "launch_path": path}
					if identity := os.Getenv("REASONIX_CUA_INSTALLED_ID"); identity != "" {
						app["bundle_id"] = identity
					}
					if launcher := os.Getenv("REASONIX_CUA_LAUNCH_PATH"); launcher != "" {
						app["launch_path"] = launcher
					}
					if launched {
						app["pid"] = pid
						app["running"] = true
					}
					entries := []any{app}
					if os.Getenv("REASONIX_CUA_DUPLICATE_INSTALLED") != "" {
						entries = append(entries, app)
					}
					if os.Getenv("REASONIX_CUA_INSTALLED_EXTRA_RUNNING") != "" {
						entries = append(entries, map[string]any{"pid": pid, "bundle_id": "com.example.Other", "running": true, "name": "Other"})
					}
					structured = map[string]any{"apps": entries}
				}
			case "launch_app":
				launched = true
				structured = map[string]any{"pid": pid, "bundle_id": os.Getenv("REASONIX_CUA_INSTALLED_APP"), "name": "Notes"}
				if identity := os.Getenv("REASONIX_CUA_INSTALLED_ID"); identity != "" {
					structured["bundle_id"] = identity
				}
			case "list_windows":
				windows := []any{map[string]any{"pid": pid, "window_id": 1, "title": "Draft", "bounds": map[string]any{"x": 100, "y": 50, "width": 2000, "height": 1000}}}
				if os.Getenv("REASONIX_CUA_TWO_WINDOWS") != "" {
					windows = append(windows, map[string]any{"pid": pid, "window_id": 2, "title": "Other"})
				}
				structured = map[string]any{"windows": windows}
			case "get_window_state":
				snapshot++
				if os.Getenv("REASONIX_CUA_BAD_REFRESH") != "" && snapshot > 1 {
					structured = map[string]any{"pid": pid, "window_id": args["window_id"], "elements": []any{map[string]any{"depth": 100}}}
					break
				}
				token := fmt.Sprintf("s%08x:2", snapshot)
				structured = map[string]any{"pid": pid, "window_id": args["window_id"], "snapshot_id": fmt.Sprint(snapshot), "capture_id": fmt.Sprint(snapshot), "elements": []any{map[string]any{"element_token": token, "role": "textField", "label": "Body", "value": value, "depth": 0}}}
				data := base64.StdEncoding.EncodeToString(fakeScreenshot())
				switch os.Getenv("REASONIX_CUA_BAD_IMAGE") {
				case "base64":
					data = "?"
				case "decode":
					data = base64.StdEncoding.EncodeToString([]byte("not an image"))
				case "missing":
					data = ""
				}
				if data != "" {
					content = append(content, map[string]any{"type": "image", "data": data, "mimeType": "image/png"})
				}
				if os.Getenv("REASONIX_CUA_BAD_TOKEN") != "" {
					structured["elements"] = []any{map[string]any{"depth": 0, "element_token": "bad]\nref"}}
				}

			case "set_value":
				value, _ = args["value"].(string)
			}
			if effect := os.Getenv("REASONIX_CUA_EFFECT"); effect != "" && req.Params.Name == "click" {
				structured["effect"] = effect
			}
			if args["reject"] == true {
				structured = map[string]any{"effect": "refused", "error": map[string]any{"code": "background_unavailable"}}
			}
			result = map[string]any{"structuredContent": structured, "content": content}
			if req.Params.Name == os.Getenv("REASONIX_CUA_OVERRIDE_TOOL") {
				var override any
				_ = json.Unmarshal([]byte(os.Getenv("REASONIX_CUA_OVERRIDE_RESULT")), &override)
				result = override
			}

		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}

func fakeSession(t *testing.T) *Session {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_CUA_FAKE", "1")
	h := NewDriver(exe, os.Environ())
	t.Cleanup(h.Close)
	return NewSession(h)
}

func snapshotRef(t *testing.T, s *Session) string {
	t.Helper()
	snap, err := s.Snapshot(t.Context(), "com.example.Notes")
	if err != nil {
		t.Fatal(err)
	}
	line := snap.Lines[0]
	a := strings.Index(line, "[")
	b := strings.Index(line, "]")
	return line[a+1 : b]
}

func TestARefusedApplicationIsRefusedBeforeTheHelperIsAsked(t *testing.T) {
	s := NewSession(NewDriver(filepath.Join(testenv.TempDir(t), "missing"), nil))
	for _, app := range []string{"com.apple.Terminal", "io.reasonix.studio", " PWSH.exe ", "SystemSettings.exe"} {
		if _, err := s.Act(t.Context(), app, []Step{{Action: "key", Key: "Enter"}}); CodeOf(err) != CodeAppRefused {
			t.Fatalf("%s: %v", app, err)
		}
	}
}

func TestCUARefsExpireOnNewSnapshotAndConnection(t *testing.T) {
	s := fakeSession(t)
	ref := snapshotRef(t, s)
	_ = snapshotRef(t, s)
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "set_value", Ref: ref, Text: "x"}}); CodeOf(err) != CodeStaleRef {
		t.Fatalf("old snapshot: %v", err)
	}
	ref = snapshotRef(t, s)
	s.driver.Close()
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "set_value", Ref: ref, Text: "x"}}); CodeOf(err) != CodeStaleRef {
		t.Fatalf("old connection: %v", err)
	}
}

func TestCUARefCannotCrossSessions(t *testing.T) {
	s := fakeSession(t)
	ref := snapshotRef(t, s)
	other := NewSession(s.driver)
	if _, err := other.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", Ref: ref}}); CodeOf(err) != CodeStaleRef {
		t.Fatal(err)
	}
}

func TestCUAExplicitWindowDoesNotSelectAnotherWindow(t *testing.T) {
	t.Setenv("REASONIX_CUA_TWO_WINDOWS", "1")
	s := fakeSession(t)
	if _, err := s.Snapshot(t.Context(), "com.example.Notes"); CodeOf(err) != CodeNoWindow {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(t.Context(), "com.example.Notes", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(t.Context(), "com.example.Notes", 3); CodeOf(err) != CodeNoWindow {
		t.Fatal(err)
	}
}

func TestCUAClickUsesFittedCapturePixelsOnce(t *testing.T) {
	log := filepath.Join(testenv.TempDir(t), "requests.jsonl")
	t.Setenv("REASONIX_CUA_REQUEST_LOG", log)
	s := fakeSession(t)
	_, _, err := s.Screenshot(t.Context(), "com.example.Notes")
	if err != nil {
		t.Fatal(err)
	}
	x, y := float64(s.state.imageWidth)/2, float64(s.state.imageHeight)/2
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", X: &x, Y: &y}}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(log)
	var click map[string]any
	for line := range strings.SplitSeq(string(body), "\n") {
		var req struct {
			Name      string
			Arguments map[string]any
		}
		_ = json.Unmarshal([]byte(line), &req)
		if req.Name == "click" {
			click = req.Arguments
		}
	}
	if click["x"] != float64(2000) || click["y"] != float64(1000) || click["delivery_mode"] != "background" || click["capture_id"] == "" {
		t.Fatalf("click: %v", click)
	}
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", X: &x, Y: &y}}); CodeOf(err) != CodeNeedsScreenshot {
		t.Fatal(err)
	}
}

func TestCUAUnverifiedEffectDoesNotStopTheBatch(t *testing.T) {
	t.Setenv("REASONIX_CUA_EFFECT", "unverifiable")
	s := fakeSession(t)
	ref := snapshotRef(t, s)
	result, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", Ref: ref}, {Action: "type", Text: "next"}})
	if err != nil || result.Done != 2 || result.FailedAt != -1 || result.Steps[0].Effect.Class != EffectUnverifiable {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestCUASuspectedNoopStopsTheBatch(t *testing.T) {
	t.Setenv("REASONIX_CUA_EFFECT", "suspected_noop")
	s := fakeSession(t)
	ref := snapshotRef(t, s)
	result, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", Ref: ref}, {Action: "type", Text: "never"}})
	if CodeOf(err) != CodeEffectUnknown || result.Done != 1 || result.FailedAt != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestCUACancelKillsUncertainConnection(t *testing.T) {
	t.Setenv("REASONIX_CUA_SILENT", "type_text")
	s := fakeSession(t)
	_ = snapshotRef(t, s)
	generation := s.driver.Generation()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "type", Text: "x"}}); err == nil {
		t.Fatal("silent driver accepted")
	}
	if s.driver.Generation() == generation || s.state.refs != nil {
		t.Fatal("cancel retained old connection or refs")
	}
}

func TestCUAUnsupportedActionsNeverUseNativeShims(t *testing.T) {
	s := fakeSession(t)
	for _, action := range []string{"focus", "hold_key", "pointer_move", "pointer_position"} {
		if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: action}}); CodeOf(err) != CodeUnsupported {
			t.Fatalf("%s: %v", action, err)
		}
	}
}

func TestWaitingIsBoundedAndTheContextEndsIt(t *testing.T) {
	s := fakeSession(t)
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "wait", Ms: 30001}}); CodeOf(err) != CodeBadStep {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "wait", Ms: 1}}); err == nil {
		t.Fatal("canceled wait accepted")
	}
}

func TestCUAOldRefCannotAliasRestartedDriverToken(t *testing.T) {
	s := fakeSession(t)
	old := snapshotRef(t, s)
	s.driver.Close()
	fresh := snapshotRef(t, s)
	if old == fresh {
		t.Fatal("reused host ref")
	}
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", Ref: old}}); CodeOf(err) != CodeStaleRef {
		t.Fatal(err)
	}
}

func TestCUAFailedRefreshClearsPreviousHandles(t *testing.T) {
	t.Setenv("REASONIX_CUA_BAD_REFRESH", "1")
	s := fakeSession(t)
	ref := snapshotRef(t, s)
	if _, err := s.Snapshot(t.Context(), "com.example.Notes"); err == nil {
		t.Fatal("malformed refresh accepted")
	}
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", Ref: ref}}); CodeOf(err) != CodeStaleRef {
		t.Fatal(err)
	}
}

func TestCUAOversizedStreamFailsWithoutCallerDeadline(t *testing.T) {
	t.Setenv("REASONIX_CUA_OVERSIZE", "1")
	s := fakeSession(t)
	before := time.Now()
	if _, err := s.Apps(context.Background()); err == nil {
		t.Fatal("oversized response accepted")
	}
	if time.Since(before) > 5*time.Second {
		t.Fatal("oversized response did not terminate")
	}
}

func TestCUARejectsMalformedObservations(t *testing.T) {
	cases := []struct {
		name, tool, response string
		shot                 bool
	}{
		{"apps type", "list_apps", `{"structuredContent":{"apps":"bad"}}`, false},
		{"missing structured", "list_apps", `{"content":[]}`, false},
		{"snapshot target", "get_window_state", `{"structuredContent":{"pid":-1,"window_id":1}}`, false},
		{"invalid windows", "list_windows", `{"structuredContent":{"windows":"bad"}}`, false},
		{"wrong process window", "list_windows", `{"structuredContent":{"windows":[{"pid":-1,"window_id":1}]}}`, false},
		{"no running apps", "list_apps", `{"structuredContent":{"apps":[{"pid":0,"running":false}]}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REASONIX_CUA_OVERRIDE_TOOL", tc.tool)
			t.Setenv("REASONIX_CUA_OVERRIDE_RESULT", tc.response)
			s := fakeSession(t)
			if _, err := s.Snapshot(t.Context(), "com.example.Notes"); err == nil {
				t.Fatal("malformed observation accepted")
			}
			if s.state.refs != nil || s.state.capture != "" {
				t.Fatal("malformed observation retained authority")
			}
		})
	}
}

func TestCUARefusesMissingOrDifferentDriver(t *testing.T) {
	t.Run("relative path", func(t *testing.T) {
		h := NewDriver("cua-driver", nil)
		var r DriverResult
		if err := h.Call(t.Context(), "list_apps", map[string]any{}, &r); CodeOf(err) != CodeUnavailable {
			t.Fatal(err)
		}
	})
	t.Run("missing binary", func(t *testing.T) {
		h := NewDriver(filepath.Join(testenv.TempDir(t), "missing"), nil)
		var r DriverResult
		if err := h.Call(t.Context(), "list_apps", map[string]any{}, &r); CodeOf(err) != CodeUnavailable {
			t.Fatal(err)
		}
	})
	t.Run("wrong version", func(t *testing.T) {
		t.Setenv("REASONIX_CUA_FAKE_VERSION", "0.35.0")
		s := fakeSession(t)
		if _, err := s.Apps(t.Context()); CodeOf(err) != CodeUnsupported {
			t.Fatal(err)
		}
	})
	t.Run("outside tool scope", func(t *testing.T) {
		s := fakeSession(t)
		if err := s.driver.Call(t.Context(), "browser_click", map[string]any{}, nil); CodeOf(err) != CodeUnsupported {
			t.Fatal(err)
		}
	})
	t.Run("embedded abort missing", func(t *testing.T) {
		h := NewEmbeddedDriver("/driver", "/private/cua.sock", "", nil)
		var r DriverResult
		if err := h.Call(t.Context(), "list_apps", map[string]any{}, &r); CodeOf(err) != CodeUnavailable {
			t.Fatal(err)
		}
	})
}

func TestCUABadActionDoesNotReachTheDriver(t *testing.T) {
	cases := []Step{
		{Action: "unknown"}, {Action: "click", Times: -1}, {Action: "click", Times: 4},
		{Action: "right_click"}, {Action: "set_value"}, {Action: "key", Times: 2},
		{Action: "scroll", Amount: 101}, {Action: "wait", Ms: -1}, {Action: "wait", Ms: 30001},
	}
	for _, step := range cases {
		t.Run(fmt.Sprintf("%s-%v-%d", step.Action, step.Amount, step.Times), func(t *testing.T) {
			s := fakeSession(t)
			res, err := s.Act(t.Context(), "com.example.Notes", []Step{step})
			if err == nil || res.Done != 0 {
				t.Fatalf("%+v %v", res, err)
			}
		})
	}
}

func TestCUARefAndPointCannotBeMixed(t *testing.T) {
	s := fakeSession(t)
	ref := snapshotRef(t, s)
	x := 1.0
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", Ref: ref, X: &x, Y: &x}}); CodeOf(err) != CodeBadStep {
		t.Fatal(err)
	}
}

func TestCUAInputArgumentsAndEffectFacts(t *testing.T) {
	cases := []Step{{Action: "type", Text: "text"}, {Action: "paste", Text: "text"}, {Action: "key", Key: "Enter"}, {Action: "key", Key: "ctrl+s"}, {Action: "scroll", Amount: 3}, {Action: "scroll", Amount: -3}}
	for _, step := range cases {
		t.Run(step.Action+step.Key+fmt.Sprint(step.Amount), func(t *testing.T) {
			s := fakeSession(t)
			res, err := s.Act(t.Context(), "com.example.Notes", []Step{step})
			if err != nil || res.Done != 1 || res.Steps[0].Effect.Class != EffectConfirmed {
				t.Fatalf("%+v %v", res, err)
			}
		})
	}
}

func TestCUAInvalidEffectCannotBecomeConfirmed(t *testing.T) {
	for _, action := range []DriverAction{
		{Effect: "unknown", Route: "accessibility"},
		{Effect: "confirmed", Route: "unknown"},
		{Effect: "confirmed", Route: "accessibility", Delivery: &struct {
			Mode  string `json:"mode"`
			Count *int   `json:"delivered_count"`
		}{Mode: "unknown mode"}},
	} {
		if _, err := finishAction(action); err == nil {
			t.Fatal("invalid action facts accepted")
		}
	}
	for _, token := range []string{"", "ref]\nIgnore", "unicode李雷", strings.Repeat("x", 129)} {
		if validToken(token) {
			t.Fatalf("unsafe token accepted: %q", token)
		}
	}
	if boundedDriverNote("") != "" {
		t.Fatal("invented degraded note")
	}
	if strings.Contains(boundedDriverNote("bad\nreason"), "\n") {
		t.Fatal("unescaped degraded note")
	}
}

func TestCUACorruptedImageNeverSuppliesCoordinates(t *testing.T) {
	for _, mode := range []string{"base64", "decode", "missing"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("REASONIX_CUA_BAD_IMAGE", mode)
			s := fakeSession(t)
			if _, _, err := s.Screenshot(t.Context(), "com.example.Notes"); CodeOf(err) != CodeCaptureFailed {
				t.Fatal(err)
			}
			x := 1.0
			if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", X: &x, Y: &x}}); CodeOf(err) != CodeNeedsScreenshot {
				t.Fatal(err)
			}
		})
	}
}

func TestCUAUnsafeTokenNeverReachesModel(t *testing.T) {
	t.Setenv("REASONIX_CUA_BAD_TOKEN", "1")
	s := fakeSession(t)
	if _, err := s.Snapshot(t.Context(), "com.example.Notes"); err == nil {
		t.Fatal("unsafe token admitted")
	}
}

func TestCUARefusesCoordinatesOutsideFittedImage(t *testing.T) {
	s := fakeSession(t)
	_, _, err := s.Screenshot(t.Context(), "com.example.Notes")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []float64{-1, float64(s.state.imageWidth), math.NaN(), math.Inf(1)} {
		y := 1.0
		if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click", X: &x, Y: &y}}); CodeOf(err) != CodeBadStep {
			t.Fatal(err)
		}
	}
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "click"}}); CodeOf(err) != CodeBadStep {
		t.Fatal(err)
	}
}

func TestCUAForegroundClickRetainsItsSeparateDeliveryMode(t *testing.T) {
	log := filepath.Join(testenv.TempDir(t), "requests.jsonl")
	t.Setenv("REASONIX_CUA_REQUEST_LOG", log)
	s := fakeSession(t)
	_, _, err := s.Screenshot(t.Context(), "com.example.Notes")
	if err != nil {
		t.Fatal(err)
	}
	x, y := 1.0, 1.0
	if _, err := s.Act(t.Context(), "com.example.Notes", []Step{{Action: "pointer_click", X: &x, Y: &y, Button: "right", Times: 2}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), `"delivery_mode":"foreground"`) || !strings.Contains(string(data), `"button":"right"`) {
		t.Fatal(string(data))
	}
}

func TestCUAFailureIdentitySurvivesDifferentDisplayDetails(t *testing.T) {
	for _, failure := range []*Failure{{Code: CodeDriverRefused}, {Code: CodeDriverRefused, Detail: "detail"}, {Code: CodeBlocked, Detail: "blocked", BlockedBy: &Modal{Title: "Save\nsecret"}}} {
		if failure.Error() == "" {
			t.Fatal("failure has no display")
		}
		if !errors.Is(failure, &Failure{Code: failure.Code}) {
			t.Fatal("failure lost identity")
		}
		if errors.Is(failure, &Failure{Code: CodeNoApp}) {
			t.Fatal("different failures collapsed")
		}
	}
	if CodeOf(errors.New("computer.driver_refused")) != "" {
		t.Fatal("classified error by wording")
	}
}

func TestCUAForegroundGrantPredicateCoversMixedBatches(t *testing.T) {
	for _, action := range []string{"pointer_move", "pointer_click", "pointer_drag", "pointer_position"} {
		if !PointerSteps([]Step{{Action: "type"}, {Action: action}}) {
			t.Fatalf("%s omitted foreground subject", action)
		}
	}
	if PointerSteps([]Step{{Action: "click"}, {Action: "wait"}}) {
		t.Fatal("ordinary batch takes pointer")
	}
	for _, identity := range []string{"windows.immersivecontrolpanel_cw5n1h2txyewy!Microsoft.Windows.Settings", "ApplicationFrameHost.exe", "electron"} {
		if Refused(identity) == "" {
			t.Fatalf("ambiguous or protected identity accepted: %s", identity)
		}
	}
}

func TestCUAFailedInterruptionDisablesReconnect(t *testing.T) {
	t.Setenv("REASONIX_CUA_SILENT", "type_text")
	s := fakeSession(t)
	s.driver.abortSocket = filepath.Join(testenv.TempDir(t), "missing.sock")
	if _, err := s.Apps(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "type", Text: "stop"}}); CodeOf(err) != CodeStopped {
		t.Fatal(err)
	}
	if _, err := s.Apps(t.Context()); CodeOf(err) != CodeStopped {
		t.Fatal("reconnected without owned stop")
	}
}

func TestCUARefusalPreservesBoundedSourceReason(t *testing.T) {
	for _, tc := range []struct{ name, response, want string }{
		{"text", `{"isError":true,"content":[{"type":"text","text":"set_value failed: AXValue is not settable"}]}`, "AXValue is not settable"},
		{"hint", `{"structuredContent":{"effect":"refused","error":{"code":"background_unavailable","hint":"exact window cannot receive background input"}}}`, "exact window cannot receive background input"},
		{"mixed", `{"structuredContent":{"effect":"refused","hint":"root hint survives","error":{"code":"target_refused"}}}`, "root hint survives"},
		{"nested reason", `{"structuredContent":{"effect":"refused","error":{"code":"target_refused","reason":"nested reason survives"}}}`, "nested reason survives"},
		{"reason", `{"structuredContent":{"effect":"refused","code":"target_refused","reason":"target rejected the write"}}`, "target rejected the write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REASONIX_CUA_OVERRIDE_TOOL", "list_apps")
			t.Setenv("REASONIX_CUA_OVERRIDE_RESULT", tc.response)
			h := fakeSession(t).driver
			var result DriverResult
			err := h.Call(t.Context(), "list_apps", map[string]any{}, &result)
			if CodeOf(err) != CodeDriverRefused || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("source cause missing: %v", err)
			}
		})
	}
}

func TestCUANativeDeliveryContractDoesNotEscalate(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       Code
	}{
		{"set_value", "background", ""},
		{"set_value", "foreground", CodeUnsupported},
		{"click", "background", CodeUnsupported},
		{"click", "foreground", CodeUnsupported},
	} {
		t.Run(tc.name+tc.mode, func(t *testing.T) {
			t.Setenv("REASONIX_CUA_NO_DELIVERY_MODE", tc.name)
			log := filepath.Join(testenv.TempDir(t), "requests")
			t.Setenv("REASONIX_CUA_REQUEST_LOG", log)
			session := fakeSession(t)
			var result DriverResult
			err := session.driver.Call(t.Context(), tc.name, map[string]any{"delivery_mode": tc.mode, "value": "value"}, &result)
			if CodeOf(err) != tc.want {
				t.Fatalf("unexpected mode projection: %v", err)
			}
			data, _ := os.ReadFile(log)
			if tc.want != "" {
				if len(data) > 0 {
					t.Fatal("unsupported delivery reached native tool")
				}
				return
			}
			var request struct {
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(data, &request); err != nil {
				t.Fatal(err)
			}
			if _, ok := request.Arguments["delivery_mode"]; ok {
				t.Fatal("schema-incompatible mode reached native tool")
			}
		})
	}
}
