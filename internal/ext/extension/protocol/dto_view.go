package protocol

import (
	"fmt"
	"strings"
)

// UINodeKind is one primitive a view is composed of. The set is deliberately
// small: a host renders these with its own components, so every addition is a
// component every frontend must then own. Nothing here describes size,
// position, or colour — an extension says what a thing is, never how big.
type UINodeKind string

const (
	UINodeText     UINodeKind = "text"
	UINodeMarkdown UINodeKind = "markdown"
	UINodeRow      UINodeKind = "row"
	UINodeStack    UINodeKind = "stack"
	UINodeKV       UINodeKind = "kv"
	UINodeMeter    UINodeKind = "meter"
	UINodePip      UINodeKind = "pip"
	UINodeButton   UINodeKind = "button"
	UINodeDivider  UINodeKind = "divider"
)

// UITone is what a node means, not what colour it is. The host owns the
// palette: an extension may say "this one is a failure" and may not say
// "failures are green".
type UITone string

const (
	UIToneDim    UITone = "dim"
	UIToneStrong UITone = "strong"
	UIToneOK     UITone = "ok"
	UIToneWarn   UITone = "warn"
	UIToneErr    UITone = "err"
	UIToneAccent UITone = "accent"
)

// Node tree limits. A published view is rendered synchronously on every
// frontend, so the bounds are part of the contract rather than a host's
// private defence: an extension that exceeds them gets an error it can fix.
const (
	MaxViewNodes  = 200
	MaxViewDepth  = 8
	MaxViewText   = 4000
	MaxViewLabel  = 200
	MaxViewSlotID = 64
)

// UINode is one node of a view. The shape is flat rather than a union because
// every consumer — the strict decoder, the JSON Schema, the generated SDKs —
// handles a discriminated struct the same way in every language. Which fields
// a kind may carry is enforced in Validate, not by the type.
type UINode struct {
	Kind UINodeKind `json:"kind"`
	// Value is the text of a text or markdown node, and the right-hand side
	// of a kv row.
	Value string `json:"value,omitempty"`
	// Key is the left-hand side of a kv row.
	Key string `json:"key,omitempty"`
	// Label names a button, or annotates a meter.
	Label string `json:"label,omitempty"`
	Tone  UITone `json:"tone,omitempty"`
	// Progress is a meter's 0..1 fill.
	Progress *float64 `json:"progress,omitempty"`
	// ActionID is the declared action a button invokes.
	ActionID string `json:"actionId,omitempty"`
	// Children are the contents of a row or a stack.
	Children []UINode `json:"children,omitempty"`
}

// UIViewPayload is a surface an extension composes out of the host's own
// primitives instead of filling a fixed shape.
type UIViewPayload struct {
	// Slot is where the view would like to stand: a name, not a position. An
	// unrecognised one degrades to the host's default place, which is what
	// lets one extension reach three frontends without knowing which.
	Slot string `json:"slot,omitempty"`
	// Anchor is "tool:<callId>": the card whose body this view replaces. Only
	// tool calls are addressable, which is what keeps an approval prompt or an
	// error state from ever being the thing redrawn (see AnchorTool).
	Anchor string   `json:"anchor,omitempty"`
	Body   []UINode `json:"body"`
}

// AnchorTool is the one anchor prefix there is, and the closed list is the
// safety property rather than an omission: nothing else in the transcript can
// be named, so nothing else can be redrawn. The host also keeps every card's
// frame and attribution, so a replaced body stays visibly the extension's.
const AnchorTool = "tool:"

// MaxAnchorLen bounds the identity a view may point at.
const MaxAnchorLen = 128

// Validate walks the tree once, checking the bounds and the fields each kind
// is allowed to carry. It runs from the strict decoder, so an extension is
// told what is wrong rather than having the offending node silently dropped.
func (p UIViewPayload) Validate() error {
	if len(p.Slot) > MaxViewSlotID {
		return fmt.Errorf("view slot is longer than %d characters", MaxViewSlotID)
	}
	if p.Anchor != "" {
		if len(p.Anchor) > MaxAnchorLen {
			return fmt.Errorf("view anchor is longer than %d characters", MaxAnchorLen)
		}
		if !strings.HasPrefix(p.Anchor, AnchorTool) || p.Anchor == AnchorTool {
			return fmt.Errorf("view anchor must name a tool call as %q", AnchorTool+"<callId>")
		}
		if p.Slot != "" {
			return fmt.Errorf("a view cannot both stand in a slot and replace a card")
		}
	}
	if len(p.Body) == 0 {
		return fmt.Errorf("view body is empty")
	}
	budget := MaxViewNodes
	for i := range p.Body {
		if err := validateNode(p.Body[i], 1, &budget); err != nil {
			return err
		}
	}
	return nil
}

func validateNode(n UINode, depth int, budget *int) error {
	if depth > MaxViewDepth {
		return fmt.Errorf("view nests deeper than %d levels", MaxViewDepth)
	}
	if *budget--; *budget < 0 {
		return fmt.Errorf("view holds more than %d nodes", MaxViewNodes)
	}
	if len(n.Value) > MaxViewText || len(n.Key) > MaxViewLabel || len(n.Label) > MaxViewLabel {
		return fmt.Errorf("view node %q carries more text than a surface can show", n.Kind)
	}
	switch n.Kind {
	case UINodeText:
		return requireValue(n, n.Value != "", "text needs a value")
	case UINodeMarkdown:
		return requireValue(n, n.Value != "", "markdown needs a value")
	case UINodeKV:
		return requireValue(n, n.Key != "", "kv needs a key")
	case UINodePip:
		return requireValue(n, n.Tone != "", "pip needs a tone to mean anything")
	case UINodeButton:
		return requireValue(n, n.ActionID != "" && n.Label != "", "button needs an actionId and a label")
	case UINodeMeter:
		if n.Progress == nil || *n.Progress < 0 || *n.Progress > 1 {
			return fmt.Errorf("meter needs a progress between 0 and 1")
		}
		return requireValue(n, true, "")
	case UINodeDivider:
		return requireValue(n, true, "")
	case UINodeRow, UINodeStack:
		if len(n.Children) == 0 {
			return fmt.Errorf("%s has no children", n.Kind)
		}
		for i := range n.Children {
			if err := validateNode(n.Children[i], depth+1, budget); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown view node kind %q", n.Kind)
	}
}

// requireValue enforces the kind's own requirement and then the rule that
// applies to every leaf: a node carries the fields of its kind and no others.
// A text node with children would render as text on one host and as a
// container on the next, which is exactly the drift the primitives exist to
// prevent.
func requireValue(n UINode, ok bool, why string) error {
	if !ok {
		return fmt.Errorf("%s", why)
	}
	if len(n.Children) > 0 {
		return fmt.Errorf("view node %q cannot have children", n.Kind)
	}
	return nil
}
