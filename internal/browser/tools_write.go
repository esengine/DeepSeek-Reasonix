package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"reasonix/internal/tool"
)

func openTool(exec Executor) tool.Tool {
	return writeTool{base: base{exec: exec, name: "browser_open",
		description: "Open a new tab at a URL for this task and return its tabId. Tabs share the task's login partition unless temporary is set. Follow with browser_snapshot before acting on the page.",
		schema:      objectSchema([]string{"operationId", "url"}, operationIDProp(), str("url", "Absolute URL to open."), boolean("temporary", "Open in an in-memory partition that shares no cookies or logins and is discarded when the tab closes.")),
		snip:        shortSnip,
	}, run: runOpen}
}

func runOpen(ctx context.Context, exec Executor, args json.RawMessage) (string, error) {
	var p struct {
		OperationID string `json:"operationId"`
		URL         string `json:"url"`
		Temporary   bool   `json:"temporary"`
	}
	if err := decode(args, &p); err != nil {
		return "", err
	}
	if err := requireOperationID(p.OperationID); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.URL) == "" {
		return "", fmt.Errorf("url is required")
	}
	tab, err := exec.Open(ctx, OpenRequest{OperationID: p.OperationID, URL: p.URL, Temporary: p.Temporary})
	if err != nil {
		return "", translate(err, "browser_open "+p.URL)
	}
	return "opened " + formatTab(tab) + "\nTake a browser_snapshot before acting on it.", nil
}

func navigateTool(exec Executor) tool.Tool {
	return writeTool{base: base{exec: exec, name: "browser_navigate",
		description: "Navigate a tab: go to a URL, or move back, forward, or reload. Navigation invalidates every earlier ref and documentToken for the tab, so take a new browser_snapshot afterwards.",
		schema: objectSchema([]string{"action", "operationId", "tabId"}, operationIDProp(), tabIDProp(),
			enum("action", "url goes to url; back, forward, and reload move through the tab's history.", NavigateURL, NavigateBack, NavigateForward, NavigateReload),
			str("url", "Absolute URL; required when action is url and not accepted otherwise.")),
		snip: shortSnip,
	}, run: runNavigate}
}

func runNavigate(ctx context.Context, exec Executor, args json.RawMessage) (string, error) {
	var p struct {
		OperationID string `json:"operationId"`
		TabID       string `json:"tabId"`
		Action      string `json:"action"`
		URL         string `json:"url"`
	}
	if err := decode(args, &p); err != nil {
		return "", err
	}
	if err := requireOperationID(p.OperationID); err != nil {
		return "", err
	}
	if err := requireTab(p.TabID); err != nil {
		return "", err
	}
	switch p.Action {
	case NavigateURL:
		if strings.TrimSpace(p.URL) == "" {
			return "", fmt.Errorf("url is required when action is url")
		}
	case NavigateBack, NavigateForward, NavigateReload:
		if p.URL != "" {
			return "", fmt.Errorf("url is only accepted when action is url")
		}
	default:
		return "", fmt.Errorf("action must be one of url, back, forward, reload")
	}
	tab, err := exec.Navigate(ctx, NavigateRequest{OperationID: p.OperationID, TabID: p.TabID, URL: p.URL, Action: p.Action})
	if err != nil {
		return "", translate(err, "browser_navigate "+p.Action+" on tab "+p.TabID)
	}
	return fmt.Sprintf("navigated (%s): %s\nEarlier refs and documentTokens for this tab are now invalid; take a new browser_snapshot.", p.Action, formatTab(tab)), nil
}

func closeTool(exec Executor) tool.Tool {
	return writeTool{base: base{exec: exec, name: "browser_close",
		description: "Close a tab. A temporary tab's partition is discarded with its last tab.",
		schema:      objectSchema([]string{"operationId", "tabId"}, operationIDProp(), tabIDProp()),
		snip:        shortSnip,
	}, run: runClose}
}

func runClose(ctx context.Context, exec Executor, args json.RawMessage) (string, error) {
	var p struct {
		OperationID string `json:"operationId"`
		TabID       string `json:"tabId"`
	}
	if err := decode(args, &p); err != nil {
		return "", err
	}
	if err := requireOperationID(p.OperationID); err != nil {
		return "", err
	}
	if err := requireTab(p.TabID); err != nil {
		return "", err
	}
	if err := exec.Close(ctx, CloseRequest{OperationID: p.OperationID, TabID: p.TabID}); err != nil {
		return "", translate(err, "browser_close tab "+p.TabID)
	}
	return "closed tab " + p.TabID, nil
}

// actArgs is the union of every reference-bound write's arguments; decodeAct
// still rejects fields the particular tool's schema does not declare.
type actArgs struct {
	OperationID   string   `json:"operationId"`
	TabID         string   `json:"tabId"`
	DocumentToken string   `json:"documentToken"`
	Ref           string   `json:"ref"`
	Text          string   `json:"text"`
	Keys          string   `json:"keys"`
	Options       []string `json:"options"`
	Files         []string `json:"files"`
	Submit        bool     `json:"submit"`
	DeltaX        int      `json:"deltaX"`
	DeltaY        int      `json:"deltaY"`
}

// actSpec describes one reference-bound write beyond the operationId, tabId,
// and documentToken every such write carries.
type actSpec struct {
	name        string
	action      string
	description string
	required    []string
	props       []property
	validate    func(actArgs) error
	describe    func(actArgs) string
}

func actTool(exec Executor, spec actSpec) tool.Tool {
	required := append([]string{"operationId", "tabId", "documentToken"}, spec.required...)
	props := append([]property{operationIDProp(), tabIDProp(), documentTokenProp()}, spec.props...)
	allowed := make(map[string]bool, len(props))
	for _, p := range props {
		allowed[p.name] = true
	}
	run := func(ctx context.Context, exec Executor, args json.RawMessage) (string, error) {
		a, err := decodeAct(args, allowed)
		if err != nil {
			return "", err
		}
		if err := spec.validate(a); err != nil {
			return "", err
		}
		what := spec.describe(a) + " on tab " + a.TabID
		res, err := exec.Act(ctx, ActRequest{
			OperationID: a.OperationID, TabID: a.TabID, DocumentToken: a.DocumentToken, Action: spec.action,
			Ref: a.Ref, Text: a.Text, Keys: a.Keys, Options: a.Options, Files: a.Files, Submit: a.Submit,
			DeltaX: a.DeltaX, DeltaY: a.DeltaY,
		})
		if err != nil {
			return "", translate(err, what)
		}
		return actOutcome(res, what)
	}
	return writeTool{base: base{exec: exec, name: spec.name, description: spec.description, schema: objectSchema(required, props...), snip: shortSnip}, run: run}
}

func decodeAct(args json.RawMessage, allowed map[string]bool) (actArgs, error) {
	var raw map[string]json.RawMessage
	if err := decode(args, &raw); err != nil {
		return actArgs{}, err
	}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		if !allowed[name] {
			return actArgs{}, fmt.Errorf("invalid args: unknown field %q", name)
		}
	}
	var a actArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return actArgs{}, fmt.Errorf("invalid args: %w", err)
	}
	if err := requireOperationID(a.OperationID); err != nil {
		return actArgs{}, err
	}
	if err := requireTab(a.TabID); err != nil {
		return actArgs{}, err
	}
	if err := requireDocumentToken(a.DocumentToken); err != nil {
		return actArgs{}, err
	}
	return a, nil
}

func actOutcome(res ActResult, what string) (string, error) {
	switch {
	case res.Outcome == OutcomeUnknown:
		return "", unknownOutcome(what)
	case res.Executed || res.Outcome == OutcomeExecuted:
		s := "executed: " + what
		if res.DocumentToken != "" {
			s += "\ndocumentToken: " + res.DocumentToken
		}
		return s, nil
	}
	return "", notExecuted(what, res.Reason)
}

func clickTool(exec Executor) tool.Tool {
	return actTool(exec, actSpec{name: "browser_click", action: ActionClick,
		description: "Click an element by its snapshot ref with trusted mouse events at its centre. Requires the documentToken of the snapshot the ref came from and a fresh operationId; if the page changed since, the call is blocked as stale and you must snapshot again.",
		required:    []string{"ref"},
		props:       []property{refProp("Element ref from browser_snapshot, for example e12.")},
		validate:    func(a actArgs) error { return requireRef(a.Ref) },
		describe:    func(a actArgs) string { return "click " + a.Ref },
	})
}

func typeTool(exec Executor) tool.Tool {
	return actTool(exec, actSpec{name: "browser_type", action: ActionType,
		description: "Type text into an element by its snapshot ref using trusted key events, so controlled inputs and custom widgets behave as they would for a user. Set submit to press Enter afterwards. Never type credentials: login pages are handed over to the user.",
		required:    []string{"ref", "text"},
		props:       []property{refProp("Element ref from browser_snapshot that accepts text."), str("text", "Text to type."), boolean("submit", "Press Enter after typing.")},
		validate: func(a actArgs) error {
			if err := requireRef(a.Ref); err != nil {
				return err
			}
			if a.Text == "" && !a.Submit {
				return fmt.Errorf("text is required unless submit is set")
			}
			return nil
		},
		describe: func(a actArgs) string { return fmt.Sprintf("type %d character(s) into %s", len(a.Text), a.Ref) },
	})
}

func pressTool(exec Executor) tool.Tool {
	return actTool(exec, actSpec{name: "browser_press", action: ActionPress,
		description: "Press a key or chord (for example Enter, Escape, Tab, Control+a) as trusted key events. Give ref to focus an element first; omit it to press on the active element.",
		required:    []string{"keys"},
		props:       []property{str("keys", "Key name or '+'-joined chord, such as Enter or Control+a."), refProp("Optional element ref from browser_snapshot to focus first.")},
		validate: func(a actArgs) error {
			if strings.TrimSpace(a.Keys) == "" {
				return fmt.Errorf("keys is required")
			}
			return nil
		},
		describe: func(a actArgs) string { return "press " + a.Keys },
	})
}

func scrollTool(exec Executor) tool.Tool {
	return actTool(exec, actSpec{name: "browser_scroll", action: ActionScroll,
		description: "Scroll the viewport, or the element named by ref, by deltaX and deltaY pixels; positive values scroll right and down. Take a new browser_snapshot afterwards to see newly revealed elements.",
		props:       []property{refProp("Optional scrollable element ref from browser_snapshot; omit to scroll the viewport."), integer("deltaX", "Horizontal pixels; positive scrolls right."), integer("deltaY", "Vertical pixels; positive scrolls down.")},
		validate: func(a actArgs) error {
			if a.DeltaX == 0 && a.DeltaY == 0 {
				return fmt.Errorf("deltaX or deltaY must be non-zero")
			}
			return nil
		},
		describe: func(a actArgs) string { return fmt.Sprintf("scroll by (%d, %d)", a.DeltaX, a.DeltaY) },
	})
}

func selectTool(exec Executor) tool.Tool {
	return actTool(exec, actSpec{name: "browser_select", action: ActionSelect,
		description: "Choose options in a select element by its snapshot ref. Give option values or visible labels; more than one only for a multi-select.",
		required:    []string{"ref", "options"},
		props:       []property{refProp("Select element ref from browser_snapshot."), strList("options", "Option values or labels to select.")},
		validate: func(a actArgs) error {
			if err := requireRef(a.Ref); err != nil {
				return err
			}
			return requireStrings("options", a.Options)
		},
		describe: func(a actArgs) string { return fmt.Sprintf("select %s in %s", strings.Join(a.Options, ", "), a.Ref) },
	})
}

func uploadTool(exec Executor) tool.Tool {
	return actTool(exec, actSpec{name: "browser_upload", action: ActionUpload,
		description: "Attach files to a file input by its snapshot ref. Only files this task owns can be attached; a remote task stages them through the task's temporary directory first.",
		required:    []string{"ref", "files"},
		props:       []property{refProp("File input ref from browser_snapshot."), strList("files", "Paths of task-owned files to attach.")},
		validate: func(a actArgs) error {
			if err := requireRef(a.Ref); err != nil {
				return err
			}
			return requireStrings("files", a.Files)
		},
		describe: func(a actArgs) string { return fmt.Sprintf("upload %d file(s) to %s", len(a.Files), a.Ref) },
	})
}
