package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func decodeView(t *testing.T, body string) (UIViewPayload, error) {
	t.Helper()
	out, err := DecodeUIPublishPayload(UISurfaceView, json.RawMessage(body))
	if err != nil {
		return UIViewPayload{}, err
	}
	view, ok := out.(UIViewPayload)
	if !ok {
		t.Fatalf("decoded %T, want UIViewPayload", out)
	}
	return view, nil
}

// The shape an extension actually publishes: a row of primitives, a meter, and
// a line of detail. Nothing in it names a size, a position, or a colour.
func TestViewDecodesAComposedSurface(t *testing.T) {
	view, err := decodeView(t, `{
      "slot": "composer-trailing",
      "body": [
        {"kind": "row", "children": [
          {"kind": "pip", "tone": "ok"},
          {"kind": "text", "value": "套餐 62%", "tone": "strong"}
        ]},
        {"kind": "meter", "progress": 0.62},
        {"kind": "text", "value": "620/1000 · 7 天后重置", "tone": "dim"},
        {"kind": "button", "actionId": "open-billing", "label": "管理套餐"}
      ]
    }`)
	if err != nil {
		t.Fatal(err)
	}
	if view.Slot != "composer-trailing" || len(view.Body) != 4 {
		t.Fatalf("view = %+v", view)
	}
	if got := view.Body[0].Children[1].Value; got != "套餐 62%" {
		t.Fatalf("nested text = %q", got)
	}
}

// A slot is a name a host may not know. The protocol takes any name because
// the list of places belongs to each frontend, not to this file — an unknown
// one degrades where it lands, which is what the host decides, not the decoder.
func TestUnknownSlotIsAcceptedAtTheProtocolLevel(t *testing.T) {
	if _, err := decodeView(t, `{"slot":"somewhere-only-one-host-has","body":[{"kind":"divider"}]}`); err != nil {
		t.Fatalf("an unfamiliar slot was rejected at decode: %v", err)
	}
}

func TestViewRejectsMalformedNodes(t *testing.T) {
	cases := []struct{ name, body, want string }{
		// Caught by the enum table before Validate ever runs, which is why the
		// registration in validate.go is the load-bearing half: without it the
		// decoder would hand a node kind nothing knows how to render.
		{"unknown kind", `{"body":[{"kind":"iframe","value":"x"}]}`, `invalid enum value "iframe"`},
		{"empty body", `{"body":[]}`, "empty"},
		{"text without value", `{"body":[{"kind":"text"}]}`, "text needs a value"},
		{"row without children", `{"body":[{"kind":"row"}]}`, "no children"},
		{"button without action", `{"body":[{"kind":"button","label":"x"}]}`, "actionId"},
		{"meter out of range", `{"body":[{"kind":"meter","progress":4}]}`, "between 0 and 1"},
		{"pip without meaning", `{"body":[{"kind":"pip"}]}`, "tone"},
		// A leaf carrying children would render as a container on one host and
		// as a leaf on the next; that drift is what the primitives prevent.
		{"leaf with children", `{"body":[{"kind":"text","value":"x","children":[{"kind":"divider"}]}]}`, "cannot have children"},
		// Style is the host's. An extension that could ask for a colour would be
		// able to paint a failure as a success.
		{"styling attempt", `{"body":[{"kind":"text","value":"x","color":"#ff0000"}]}`, "do not match"},
		{"unknown tone", `{"body":[{"kind":"text","value":"x","tone":"neon"}]}`, "tone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeView(t, tc.body); err == nil {
				t.Fatal("accepted a malformed view")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The bounds are part of the contract because every frontend renders a
// published view synchronously: a tree that is too deep or too large is a
// frozen window, so the extension is told rather than the host defending.
func TestViewBoundsAreEnforced(t *testing.T) {
	deep := `{"kind":"divider"}`
	for range MaxViewDepth + 1 {
		deep = fmt.Sprintf(`{"kind":"stack","children":[%s]}`, deep)
	}
	if _, err := decodeView(t, `{"body":[`+deep+`]}`); err == nil {
		t.Error("a tree deeper than the limit was accepted")
	}

	wide := make([]string, MaxViewNodes+1)
	for i := range wide {
		wide[i] = `{"kind":"divider"}`
	}
	if _, err := decodeView(t, `{"body":[`+strings.Join(wide, ",")+`]}`); err == nil {
		t.Error("a tree with more nodes than the limit was accepted")
	}

	if _, err := decodeView(t, `{"body":[{"kind":"text","value":"`+strings.Repeat("x", MaxViewText+1)+`"}]}`); err == nil {
		t.Error("a text node past the length limit was accepted")
	}
}

// The anchor allow-list is the whole safety property of takeover: only a tool
// call can be pointed at, so an approval prompt, a permission decision or an
// error state is not addressable no matter what an extension sends.
func TestOnlyToolCallsCanBeAnchored(t *testing.T) {
	if _, err := decodeView(t, `{"anchor":"tool:call_42","body":[{"kind":"divider"}]}`); err != nil {
		t.Fatalf("a tool anchor was rejected: %v", err)
	}
	for _, anchor := range []string{"approval:1", "permission:bash", "message:7", "tool:", "call_42"} {
		body := `{"anchor":"` + anchor + `","body":[{"kind":"divider"}]}`
		if _, err := decodeView(t, body); err == nil {
			t.Errorf("anchor %q was accepted; only tool calls may be replaced", anchor)
		}
	}
}

// Standing somewhere and standing in for something are different jobs. A view
// that claimed both would have no single answer to "where does this go".
func TestAViewIsEitherPlacedOrAnchored(t *testing.T) {
	body := `{"slot":"composer-trailing","anchor":"tool:call_42","body":[{"kind":"divider"}]}`
	if _, err := decodeView(t, body); err == nil {
		t.Fatal("a view claimed both a slot and an anchor")
	}
}
