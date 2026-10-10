package computer

import (
	"slices"
	"strings"
)

const maxCandidates = 10

// withWindow adds the window a request is aimed at; zero leaves the helper to
// its default, the application's front window.
func withWindow(params map[string]any, window int) map[string]any {
	if window != 0 {
		params["window"] = window
	}
	return params
}

// needsWindow reports whether any step goes to one window of the application
// rather than to an element a ref already names.
func needsWindow(steps []Step) bool {
	return slices.ContainsFunc(steps, func(s Step) bool {
		switch strings.ToLower(strings.TrimSpace(s.Action)) {
		case "type", "paste", "key", "hold_key", "pointer_move", "pointer_click", "pointer_drag":
			return true
		case "click", "scroll":
			return s.Ref == ""
		}
		return false
	})
}

// target settles which window an operation is aimed at: one the model names
// must be the application's own and a helper that classifies windows must be
// able to aim at it; with none named, two or more independent
// windows are refused before anything is sent. A dialog another window owns is
// not a second window, and a helper that does not classify windows judges none.
func (a App) target(window int, scoped bool) error {
	if window != 0 {
		if slices.ContainsFunc(a.Windows, func(w Window) bool { return w.Owned == nil }) {
			return fail(CodeUnsupported, "%s cannot be aimed at one of its windows on this platform; leave window out", ShownBundle(a.Bundle))
		}
		if !slices.ContainsFunc(a.Windows, func(w Window) bool { return w.ID == window }) {
			return fail(CodeNoWindow, "window %d is not a window of %s; list the applications for its windows", window, ShownBundle(a.Bundle))
		}
		return nil
	}
	if !scoped {
		return nil
	}
	var independent []Window
	for _, w := range a.Windows {
		if w.Owned == nil {
			return nil
		}
		if !*w.Owned {
			independent = append(independent, w)
		}
	}
	if len(independent) < 2 {
		return nil
	}
	f := fail(CodeAmbiguousWindow, "%s has %d windows and this would go to one of them; repeat the call with window set to the one meant", ShownBundle(a.Bundle), len(independent))
	f.Candidates = independent
	return f
}
