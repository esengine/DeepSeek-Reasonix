package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/computer"
	"reasonix/internal/safety/permission"
)

func init() {
	tool.RegisterBuiltin(computerRead{})
	tool.RegisterBuiltin(computerAct{})
}

const computerSnapshotLines = 300

// ComputerTools binds the computer-use tools to the machine's helper.
func ComputerTools(session *computer.Session) []tool.Tool {
	return []tool.Tool{computerRead{session: session}, computerAct{session: session}}
}

// ComputerBound reports whether t is a computer-use tool with a helper behind it.
func ComputerBound(t tool.Tool) bool {
	switch c := t.(type) {
	case computerRead:
		return c.session != nil
	case computerAct:
		return c.session != nil
	}
	return false
}

// computerAppsSubject is the subject of listing every application, which is a
// question of its own rather than one any application's grant answers.
const computerAppsSubject = "apps"

var errNoComputer = &computer.Failure{Code: computer.CodeUnavailable, Detail: "computer use runs only in Reasonix Studio on macOS and Windows"}

type computerRead struct{ session *computer.Session }

func (computerRead) Name() string { return "computer_read" }

func (computerRead) Description() string {
	return "See another application on this computer, named as what=apps lists it. what=apps lists applications with windows; snapshot returns its accessibility tree with [aN] refs for computer_act; screenshot captures its front window (computer_act x/y use its pixels, and the person is shown it too)."
}

func (computerRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"what":{"type":"string","enum":["apps","snapshot","screenshot"]},"app":{"type":"string"}},"required":["what"]}`)
}

func (computerRead) ReadOnly() bool                                   { return false }
func (computerRead) Sequential(context.Context, json.RawMessage) bool { return true }

// PermissionArgs names the application read, or the listing.
func (c computerRead) PermissionArgs(_ context.Context, args json.RawMessage) json.RawMessage {
	var p struct {
		What string `json:"what"`
		App  string `json:"app"`
	}
	_ = json.Unmarshal(args, &p)
	if p.What == "apps" {
		p.App = computerAppsSubject
	}
	return hostSubjectArgs(c.Schema(), args, "app", strings.TrimSpace(p.App))
}

func (c computerRead) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, _, err := c.ExecuteWithImages(ctx, args)
	return text, err
}

func (c computerRead) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var p struct {
		What string `json:"what"`
		App  string `json:"app"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", nil, fmt.Errorf("invalid args: %w", err)
	}
	if c.session == nil {
		return "", nil, errNoComputer
	}
	switch p.What {
	case "apps":
		apps, err := c.session.Apps(ctx)
		if err != nil {
			return "", nil, err
		}
		return renderApps(apps), nil, nil
	case "snapshot":
		snap, err := c.session.Snapshot(ctx, p.App)
		if err != nil {
			return "", nil, err
		}
		return renderComputerSnapshot(snap), nil, nil
	case "screenshot":
		shot, app, err := c.session.Screenshot(ctx, p.App)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("%s (%s): front window\n[image: screenshot]", app.Name, app.Bundle), []string{shot}, nil
	}
	return "", nil, &computer.Failure{Code: computer.CodeBadStep, Detail: fmt.Sprintf("what=%q is not one of apps, snapshot, screenshot", p.What)}
}

type computerAct struct{ session *computer.Session }

func (computerAct) Name() string { return "computer_act" }

func (computerAct) Description() string {
	return "Operate another application. Steps run in order and stop at the first failure. Through its accessibility actions, leaving the pointer alone: " +
		"click (ref, or x/y from a screenshot), right_click opens a ref's context menu, focus and set_value take a ref, type enters text where focus is and paste puts it there at once (the clipboard is borrowed and put back), key presses keys like Enter or Meta+s (control+s on Windows; times repeats), hold_key holds one for seconds. " +
		"Paste needs the application in front on macOS; on Windows type, paste and key bring it forward. scroll brings a ref into view or turns the wheel by amount lines, wait pauses ms. " +
		"pointer_move, pointer_click (button, times), pointer_drag (to_x/to_y) and pointer_position instead take the person's pointer and bring the application forward, approved separately; use them for what has no accessibility action, and the pointer goes back where it was. " +
		"Returns a snapshot afterwards."
}

func (computerAct) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"app":{"type":"string"},"steps":{"type":"array","items":{"type":"object","properties":{"action":{"type":"string","enum":["click","right_click","focus","set_value","type","paste","key","hold_key","scroll","wait","pointer_move","pointer_click","pointer_drag","pointer_position"]},"ref":{"type":"string"},"text":{"type":"string"},"key":{"type":"string"},"x":{"type":"number"},"y":{"type":"number"},"amount":{"type":"number"},"ms":{"type":"integer"},"to_x":{"type":"number"},"to_y":{"type":"number"},"button":{"type":"string","enum":["left","right","middle"]},"times":{"type":"integer"},"seconds":{"type":"number"}},"required":["action"]}}},"required":["app","steps"]}`)
}

func (computerAct) ReadOnly() bool                                   { return false }
func (computerAct) Sequential(context.Context, json.RawMessage) bool { return true }

// PermissionArgs names the application operated.
func (c computerAct) PermissionArgs(_ context.Context, args json.RawMessage) json.RawMessage {
	var p struct {
		App   string          `json:"app"`
		Steps []computer.Step `json:"steps"`
	}
	_ = json.Unmarshal(args, &p)
	app := strings.TrimSpace(p.App)
	if app != "" && computer.PointerSteps(p.Steps) {
		app = permission.ComputerPointerPrefix + app
	}
	return hostSubjectArgs(c.Schema(), args, "app", app)
}

func (c computerAct) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		App   string          `json:"app"`
		Steps []computer.Step `json:"steps"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if c.session == nil {
		return "", errNoComputer
	}
	if len(p.Steps) == 0 {
		return "", &computer.Failure{Code: computer.CodeBadStep, Detail: "steps is empty"}
	}
	res, stepErr := c.session.Act(ctx, p.App, p.Steps)
	var out strings.Builder
	fmt.Fprintf(&out, "Completed %d of %d step(s).\n", res.Done, len(p.Steps))
	for i, note := range res.Notes {
		fmt.Fprintf(&out, "  %d. %s\n", i+1, note)
	}
	if stepErr != nil {
		fmt.Fprintf(&out, "Step %d failed: %v\n", res.FailedAt+1, stepErr)
	}
	if res.App.Bundle == "" {
		return out.String(), stepErr
	}
	snap, err := c.session.Snapshot(ctx, res.App.Bundle)
	if err != nil {
		fmt.Fprintf(&out, "The application could not be read afterwards: %v\n", err)
		return out.String(), stepErr
	}
	out.WriteString("Now:\n" + renderComputerSnapshot(snap))
	return out.String(), stepErr
}

func renderApps(apps []computer.App) string {
	var b strings.Builder
	for _, app := range apps {
		mark := " "
		if app.Active {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s %s — %s", mark, app.Bundle, app.Name)
		if why := computer.Refused(app.Bundle); why != "" {
			fmt.Fprintf(&b, " (never operated: %s)", why)
		}
		b.WriteString("\n")
		for _, w := range app.Windows {
			title := w.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(&b, "    window %q %.0f×%.0f\n", title, w.Bounds.Width, w.Bounds.Height)
		}
	}
	if b.Len() == 0 {
		return "No applications are running with windows a person can see.\n"
	}
	return b.String()
}

func renderComputerSnapshot(snap computer.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", snap.App.Name, snap.App.Bundle)
	if snap.Note != "" {
		b.WriteString(snap.Note + "\n")
	}
	lines := snap.Lines
	if len(lines) > computerSnapshotLines {
		lines = lines[:computerSnapshotLines]
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	if snap.Truncated || len(snap.Lines) > len(lines) {
		b.WriteString("… the tree is longer than this; act on what is shown or take a screenshot\n")
	}
	return b.String()
}
