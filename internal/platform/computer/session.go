package computer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"slices"
	"strings"
	"sync"
	"time"

	"reasonix/internal/model/visionimage"
)

// maxWait bounds a wait step: a pause longer than this is a task that should
// come back rather than one call holding the turn.
const maxWait = 30 * time.Second

// refusedApps are never operated, whatever the person approves: input there
// reaches past the boundaries the host keeps everywhere else. A security list,
// kept as one: macOS bundle ids, and Windows process file names or a Store
// application's package family name.
var refusedApps = map[string]string{
	"io.reasonix.studio":          "Reasonix Studio itself",
	"com.github.Electron":         "Reasonix Studio itself",
	"com.apple.Terminal":          "a terminal",
	"com.googlecode.iterm2":       "a terminal",
	"dev.warp.Warp-Stable":        "a terminal",
	"net.kovidgoyal.kitty":        "a terminal",
	"com.mitchellh.ghostty":       "a terminal",
	"org.alacritty":               "a terminal",
	"com.apple.keychainaccess":    "the keychain",
	"com.apple.Passwords":         "a password manager",
	"com.1password.1password":     "a password manager",
	"com.agilebits.onepassword7":  "a password manager",
	"com.bitwarden.desktop":       "a password manager",
	"com.apple.systempreferences": "System Settings",
	"com.apple.SecurityAgent":     "a system authorization prompt",

	"reasonix studio.exe":                            "Reasonix Studio itself",
	"electron.exe":                                   "Reasonix Studio itself",
	"windowsterminal.exe":                            "a terminal",
	"microsoft.windowsterminal_8wekyb3d8bbwe":        "a terminal",
	"microsoft.windowsterminalpreview_8wekyb3d8bbwe": "a terminal",
	"openconsole.exe":                                "a terminal",
	"conhost.exe":                                    "a terminal",
	"cmd.exe":                                        "a terminal",
	"powershell.exe":                                 "a terminal",
	"pwsh.exe":                                       "a terminal",
	"mintty.exe":                                     "a terminal",
	"wezterm-gui.exe":                                "a terminal",
	"alacritty.exe":                                  "a terminal",
	"1password.exe":                                  "a password manager",
	"bitwarden.exe":                                  "a password manager",
	"keepass.exe":                                    "a password manager",
	"keepassxc.exe":                                  "a password manager",
	"credentialuibroker.exe":                         "a system authorization prompt",
	"consent.exe":                                    "a system authorization prompt",
	"windows.immersivecontrolpanel_cw5n1h2txyewy": "Windows Settings",
	"systemsettings.exe":                          "Windows Settings",
	"regedit.exe":                                 "the registry editor",
	"mmc.exe":                                     "a system management console",
	"taskmgr.exe":                                 "Task Manager",
	"powershell_ise.exe":                          "a terminal",
	"conemu.exe":                                  "a terminal",
	"conemu64.exe":                                "a terminal",
	"wt.exe":                                      "a terminal",
	"microsoft.powershell_8wekyb3d8bbwe":          "a terminal",
	"microsoft.sechealthui_8wekyb3d8bbwe":         "Windows Security",
}

// refusedFolded is refusedApps compared without case: Windows file names are
// case-insensitive, and a refusal must not be one capital letter from missing.
var refusedFolded = func() map[string]string {
	m := make(map[string]string, len(refusedApps))
	for name, why := range refusedApps {
		m[strings.ToLower(name)] = why
	}
	return m
}()

// Refused reports why an application is never operated, or "" when it may be.
func Refused(bundle string) string { return refusedFolded[strings.ToLower(strings.TrimSpace(bundle))] }

// App is a running application a person can see.
type App struct {
	PID    int32  `json:"pid"`
	Bundle string `json:"bundle"`
	// Exe is a Windows application's file name when Bundle is its package
	// family name; a refusal holds under either.
	Exe     string   `json:"exe,omitempty"`
	Name    string   `json:"name"`
	Active  bool     `json:"active"`
	Windows []Window `json:"windows"`
}

// Window is one of an application's windows on screen, in global points.
type Window struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Bounds Rect   `json:"bounds"`
}

// Rect is a rectangle in global points, origin top-left.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Session is the computer as one agent sees it: the helper it shares with every
// other, and the geometry of the screenshots it was shown.
type Session struct {
	helper *Helper

	mu    sync.Mutex
	shots map[string]shotGeometry
}

type shotGeometry struct {
	bounds Rect
	scale  float64 // screen points per pixel of the image the model was shown
}

// NewSession drives applications through helper.
func NewSession(helper *Helper) *Session {
	return &Session{helper: helper, shots: map[string]shotGeometry{}}
}

// Apps lists the applications a person can see, refused ones included and
// marked by the caller.
func (s *Session) Apps(ctx context.Context) ([]App, error) {
	var r struct {
		Apps []App `json:"apps"`
	}
	if err := s.call(ctx, "apps", nil, &r); err != nil {
		return nil, err
	}
	return r.Apps, nil
}

// App resolves a bundle id to the running application, refusing the ones that
// are never operated.
func (s *Session) App(ctx context.Context, bundle string) (App, error) {
	bundle = strings.TrimSpace(bundle)
	if why := Refused(bundle); why != "" {
		return App{}, fail(CodeAppRefused, "%s is %s, which the agent never operates", bundle, why)
	}
	apps, err := s.Apps(ctx)
	if err != nil {
		return App{}, err
	}
	for _, app := range apps {
		if app.Bundle != bundle {
			continue
		}
		if why := Refused(app.Exe); why != "" {
			return App{}, fail(CodeAppRefused, "%s is %s (%s), which the agent never operates", bundle, why, app.Exe)
		}
		return app, nil
	}
	return App{}, fail(CodeNoApp, "no running application has the bundle id %q; list apps first", bundle)
}

// Snapshot is an application's accessibility tree, one element per line. Note
// is the host's account of what the tree is missing, such as an application
// that exposes no window at all.
type Snapshot struct {
	App       App
	Lines     []string
	Truncated bool
	Note      string
}

func (s *Session) Snapshot(ctx context.Context, bundle string) (Snapshot, error) {
	app, err := s.App(ctx, bundle)
	if err != nil {
		return Snapshot{}, err
	}
	var r struct {
		Lines     []string `json:"lines"`
		Truncated bool     `json:"truncated"`
		Note      string   `json:"note"`
	}
	if err := s.call(ctx, "snapshot", map[string]any{"pid": app.PID}, &r); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{App: app, Lines: r.Lines, Truncated: r.Truncated, Note: r.Note}, nil
}

// Screenshot captures an application's front window, sized for a vision model.
// x and y in later steps are read in this image's pixels.
func (s *Session) Screenshot(ctx context.Context, bundle string) (string, App, error) {
	app, err := s.App(ctx, bundle)
	if err != nil {
		return "", App{}, err
	}
	var r struct {
		Data   string `json:"data"`
		Mime   string `json:"mime"`
		Bounds Rect   `json:"bounds"`
	}
	if err := s.call(ctx, "screenshot", map[string]any{"pid": app.PID}, &r); err != nil {
		return "", App{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		return "", App{}, fail(CodeCaptureFailed, "the capture was not valid base64")
	}
	fitted, mime, err := visionimage.Fit(raw, r.Mime)
	if err != nil {
		return "", App{}, fail(CodeCaptureFailed, "%v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(fitted))
	if err != nil || cfg.Width == 0 {
		return "", App{}, fail(CodeCaptureFailed, "the capture could not be measured")
	}
	s.mu.Lock()
	s.shots[app.Bundle] = shotGeometry{bounds: r.Bounds, scale: r.Bounds.Width / float64(cfg.Width)}
	s.mu.Unlock()
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(fitted), app, nil
}

// Step is one input to an application.
type Step struct {
	Action string   `json:"action"`
	Ref    string   `json:"ref,omitempty"`
	Text   string   `json:"text,omitempty"`
	Key    string   `json:"key,omitempty"`
	X      *float64 `json:"x,omitempty"`
	Y      *float64 `json:"y,omitempty"`
	// scroll: lines to turn the wheel by, negative downwards; wait: how long.
	Amount float64 `json:"amount,omitempty"`
	Ms     int     `json:"ms,omitempty"`
	// Where a pointer drag ends, which button a pointer click uses, and how many
	// times a click or a key repeats.
	ToX    *float64 `json:"to_x,omitempty"`
	ToY    *float64 `json:"to_y,omitempty"`
	Button string   `json:"button,omitempty"`
	Times  int      `json:"times,omitempty"`
	// hold_key: how long the key stays down.
	Seconds float64 `json:"seconds,omitempty"`
}

// PointerStep reports whether a step takes the person's pointer rather than
// going through the application's own accessibility actions. The two are
// authorised apart: one asks an element to act, the other moves the pointer the
// person is holding.
func PointerStep(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "pointer_move", "pointer_click", "pointer_drag", "pointer_position":
		return true
	}
	return false
}

// PointerSteps reports whether any of these steps takes the pointer.
func PointerSteps(steps []Step) bool {
	return slices.ContainsFunc(steps, func(s Step) bool { return PointerStep(s.Action) })
}

// ActResult is what a run of steps did before it finished or stopped.
type ActResult struct {
	App      App
	Done     int
	Notes    []string
	FailedAt int
}

// Act runs steps in order on one application and stops at the first that
// fails, or when the person presses Escape while the agent's cursor is showing.
func (s *Session) Act(ctx context.Context, bundle string, steps []Step) (ActResult, error) {
	res := ActResult{FailedAt: -1}
	app, err := s.App(ctx, bundle)
	if err != nil {
		res.FailedAt = 0
		return res, err
	}
	res.App = app
	// The pointer goes back where the person left it when the steps are done,
	// whether they finished or stopped.
	if PointerSteps(steps) {
		defer func() {
			release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = s.helper.Call(release, "pointer_release", nil, nil)
			cancel()
		}()
	}
	started := s.helper.stopMark()
	for i, step := range steps {
		if s.helper.stoppedSince(started) {
			res.FailedAt = i
			return res, fail(CodeStopped, "the person pressed Escape; ask before continuing")
		}
		note, err := s.step(ctx, app, step)
		if err != nil {
			res.FailedAt = i
			return res, err
		}
		res.Done++
		res.Notes = append(res.Notes, note)
	}
	return res, nil
}

func (s *Session) step(ctx context.Context, app App, step Step) (string, error) {
	pid := app.PID
	switch strings.ToLower(strings.TrimSpace(step.Action)) {
	case "click":
		if step.Ref != "" {
			return "press " + step.Ref, s.call(ctx, "press", map[string]any{"pid": pid, "ref": step.Ref}, nil)
		}
		x, y, err := s.screenPoint(app, step)
		if err != nil {
			return "", err
		}
		var r struct {
			Role string `json:"role"`
		}
		if err := s.call(ctx, "click", map[string]any{"pid": pid, "x": x, "y": y}, &r); err != nil {
			return "", err
		}
		return fmt.Sprintf("click the %s at (%v,%v)", r.Role, *step.X, *step.Y), nil
	case "focus":
		return "focus " + step.Ref, s.call(ctx, "focus", map[string]any{"pid": pid, "ref": step.Ref}, nil)
	case "set_value":
		return fmt.Sprintf("set %s to %d characters", step.Ref, len([]rune(step.Text))),
			s.call(ctx, "set_value", map[string]any{"pid": pid, "ref": step.Ref, "text": step.Text}, nil)
	case "type":
		return fmt.Sprintf("type %d characters", len([]rune(step.Text))), s.call(ctx, "type", map[string]any{"pid": pid, "text": step.Text}, nil)
	case "key":
		return "press " + step.Key, s.call(ctx, "key", map[string]any{"pid": pid, "key": step.Key, "times": max(step.Times, 1)}, nil)
	case "paste":
		return fmt.Sprintf("paste %d characters", len([]rune(step.Text))),
			s.call(ctx, "paste", map[string]any{"pid": pid, "text": step.Text}, nil)
	case "hold_key":
		return fmt.Sprintf("hold %s for %vs", step.Key, step.Seconds),
			s.call(ctx, "hold_key", map[string]any{"pid": pid, "key": step.Key, "seconds": step.Seconds}, nil)
	case "pointer_move", "pointer_click", "pointer_drag":
		return s.pointerStep(ctx, app, step)
	case "pointer_position":
		var at struct{ X, Y float64 }
		if err := s.call(ctx, "pointer_position", nil, &at); err != nil {
			return "", err
		}
		return s.describePoint(app, at.X, at.Y), nil
	case "right_click":
		if step.Ref == "" {
			return "", fail(CodeBadStep, "a right_click needs a ref; the menu belongs to the element")
		}
		return "open the menu of " + step.Ref, s.call(ctx, "menu", map[string]any{"pid": pid, "ref": step.Ref}, nil)
	case "scroll":
		var r struct {
			How string `json:"how"`
		}
		if err := s.call(ctx, "scroll", map[string]any{"pid": pid, "ref": step.Ref, "amount": step.Amount}, &r); err != nil {
			return "", err
		}
		if r.How == "revealed" {
			return "bring " + step.Ref + " into view", nil
		}
		return fmt.Sprintf("scroll %v lines", step.Amount), nil
	case "wait":
		d := min(time.Duration(step.Ms)*time.Millisecond, maxWait)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "wait " + d.String(), nil
	}
	return "", fail(CodeBadStep, "unknown action %q; use click, right_click, focus, set_value, type, paste, key, hold_key, scroll, wait, or the pointer steps pointer_move, pointer_click and pointer_drag", step.Action)
}

// pointerStep takes the person's pointer to the point a screenshot named. The
// helper refuses a point whose window belongs to another application, so an
// approval for one application cannot reach into the next.
func (s *Session) pointerStep(ctx context.Context, app App, step Step) (string, error) {
	x, y, err := s.screenPoint(app, step)
	if err != nil {
		return "", err
	}
	args := map[string]any{"pid": app.PID, "x": x, "y": y}
	switch strings.ToLower(strings.TrimSpace(step.Action)) {
	case "pointer_move":
		return fmt.Sprintf("move the pointer to (%v,%v)", *step.X, *step.Y), s.call(ctx, "pointer_move", args, nil)
	case "pointer_drag":
		to := Step{X: step.ToX, Y: step.ToY}
		if to.X == nil || to.Y == nil {
			return "", fail(CodeBadStep, "a pointer_drag needs where it ends: to_x and to_y")
		}
		toX, toY, err := s.screenPoint(app, to)
		if err != nil {
			return "", err
		}
		args["to_x"], args["to_y"] = toX, toY
		return fmt.Sprintf("drag the pointer from (%v,%v) to (%v,%v)", *step.X, *step.Y, *to.X, *to.Y), s.call(ctx, "pointer_drag", args, nil)
	}
	button := step.Button
	if button == "" {
		button = "left"
	}
	args["button"], args["clicks"] = button, max(step.Times, 1)
	return fmt.Sprintf("%s click at (%v,%v)", button, *step.X, *step.Y), s.call(ctx, "pointer_click", args, nil)
}

// describePoint says where a point on screen is in the pixels of the
// application's latest screenshot, which is the only frame the model has. With
// no screenshot taken there is no such frame, and the screen's own is said.
func (s *Session) describePoint(app App, x, y float64) string {
	s.mu.Lock()
	shot, ok := s.shots[app.Bundle]
	s.mu.Unlock()
	if !ok || shot.scale == 0 {
		return fmt.Sprintf("the pointer is at (%.0f,%.0f) on screen; take a screenshot to place it in this application", x, y)
	}
	return fmt.Sprintf("the pointer is at (%.0f,%.0f) in the screenshot", (x-shot.bounds.X)/shot.scale, (y-shot.bounds.Y)/shot.scale)
}

// screenPoint converts a point in the latest screenshot's pixels to global
// screen points.
func (s *Session) screenPoint(app App, step Step) (float64, float64, error) {
	if step.X == nil || step.Y == nil {
		return 0, 0, fail(CodeBadStep, "a click needs a ref, or x and y from a screenshot")
	}
	s.mu.Lock()
	shot, ok := s.shots[app.Bundle]
	s.mu.Unlock()
	if !ok {
		return 0, 0, fail(CodeNeedsScreenshot, "take a screenshot of %s first; x and y are read in its pixels", app.Bundle)
	}
	return shot.bounds.X + *step.X*shot.scale, shot.bounds.Y + *step.Y*shot.scale, nil
}

var permissionNames = map[string]string{"accessibility": "Accessibility", "screen_recording": "Screen Recording"}

// call runs one helper request. A permission the system has not granted is
// asked for once per helper, which opens the settings pane where it is granted,
// and the failure tells the model what the person has to do there.
func (s *Session) call(ctx context.Context, method string, params, out any) error {
	err := s.helper.Call(ctx, method, params, out)
	f, ok := errors.AsType[*Failure](err)
	if !ok || f.Code != CodePermissionMissing {
		return err
	}
	name, known := permissionNames[f.Detail]
	if !known {
		return err
	}
	if s.helper.firstAsk(f.Detail) {
		_ = s.helper.Call(ctx, "request_permission", map[string]any{"which": f.Detail}, nil)
	}
	return fail(CodePermissionMissing, "Reasonix Studio has not been granted %s. Ask the person to switch it on under System Settings → Privacy & Security → %s, then try again", name, name)
}
