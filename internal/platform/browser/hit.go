package browser

import (
	"context"
	"fmt"
	"strconv"
)

// maxLabels bounds how many labels of one control are tried as click points.
const maxLabels = 4

// viewport is the page's layout viewport in CSS pixels, and where it sits in
// the document. Inputs and content quads are in viewport coordinates;
// DOM.getNodeForLocation takes document coordinates.
type viewport struct {
	Width  float64 `json:"clientWidth"`
	Height float64 `json:"clientHeight"`
	PageX  float64 `json:"pageX"`
	PageY  float64 `json:"pageY"`
}

func (t *tab) viewport(ctx context.Context) (viewport, error) {
	var metrics struct {
		Viewport viewport `json:"cssLayoutViewport"`
	}
	if err := t.call(ctx, "Page.getLayoutMetrics", nil, &metrics); err != nil {
		return viewport{}, engineFailure(err)
	}
	return metrics.Viewport, nil
}

// point is where an input for step lands, in CSS pixels. For a ref it is a
// point where a real pointer would reach the element (see reach).
func (s *Session) point(ctx context.Context, t *tab, step Step) (float64, float64, error) {
	if step.Ref == "" {
		if step.X == nil || step.Y == nil {
			return 0, 0, fail(CodeBadStep, "a %s step needs a ref, or x and y from a screenshot", step.Action)
		}
		scale := t.screenshotScale()
		return *step.X * scale, *step.Y * scale, nil
	}
	node, err := s.node(ctx, t, step.Ref)
	if err != nil {
		return 0, 0, err
	}
	return s.reach(ctx, t, node, step.Ref)
}

// reach tries the element's own visible middle, first where scrolling it into
// view leaves it and then scrolled to the middle of the viewport, which clears
// a fixed header or footer. Past that it tries the labels that activate it: a
// form control is often drawn by its label with the control itself hidden or
// drawn over, and a person clicks what is drawn.
func (s *Session) reach(ctx context.Context, t *tab, node int64, ref string) (float64, float64, error) {
	// The failure told is the target's own after its last scroll, unless it
	// has no box and a label of it is what was drawn over.
	var failure error
	try := func(aims []int64, own bool) (float64, float64, bool, error) {
		for _, aim := range aims {
			for _, centre := range []bool{false, true} {
				x, y, err := s.aim(ctx, t, node, aim, ref, centre)
				if err == nil {
					return x, y, true, nil
				}
				switch CodeOf(err) {
				case CodeCovered, CodeNotVisible:
					if own || CodeOf(failure) == CodeNotVisible && CodeOf(err) == CodeCovered {
						failure = err
					}
				default:
					return 0, 0, false, err
				}
			}
		}
		return 0, 0, false, nil
	}
	if x, y, ok, err := try([]int64{node}, true); ok || err != nil {
		return x, y, err
	}
	labels, err := t.labels(ctx, node)
	if err != nil {
		return 0, 0, err
	}
	if x, y, ok, err := try(labels, false); ok || err != nil {
		return x, y, err
	}
	return 0, 0, failure
}

// aim scrolls one element into view and checks that a pointer at its visible
// middle would reach target.
func (s *Session) aim(ctx context.Context, t *tab, target, aim int64, ref string, centre bool) (float64, float64, error) {
	if centre {
		obj, err := t.resolveObject(ctx, aim)
		if err != nil {
			return 0, 0, err
		}
		if err := t.call(ctx, "Runtime.callFunctionOn", map[string]any{
			"objectId":            obj,
			"functionDeclaration": `function(){this.scrollIntoView({block:"center",inline:"center",behavior:"instant"})}`,
		}, nil); err != nil {
			return 0, 0, engineFailure(err)
		}
	} else if err := t.call(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{"backendNodeId": aim}, nil); err != nil {
		if isProtocolError(err) {
			return 0, 0, &Failure{Code: CodeNotVisible, Ref: ref, Detail: fmt.Sprintf("%s has no box on the page (hidden or not rendered)", ref)}
		}
		return 0, 0, engineFailure(err)
	}
	x, y, vp, err := t.visibleCenter(ctx, aim, ref)
	if err != nil {
		return 0, 0, err
	}
	if err := s.checkHit(ctx, t, target, ref, x, y, vp); err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

func (t *tab) visibleCenter(ctx context.Context, node int64, ref string) (float64, float64, viewport, error) {
	var quads struct {
		Quads [][]float64 `json:"quads"`
	}
	if err := t.call(ctx, "DOM.getContentQuads", map[string]any{"backendNodeId": node}, &quads); err != nil && !isProtocolError(err) {
		return 0, 0, viewport{}, engineFailure(err)
	}
	vp, err := t.viewport(ctx)
	if err != nil {
		return 0, 0, viewport{}, err
	}
	for _, q := range quads.Quads {
		if len(q) != 8 {
			continue
		}
		minX, maxX := min(q[0], q[2], q[4], q[6]), max(q[0], q[2], q[4], q[6])
		minY, maxY := min(q[1], q[3], q[5], q[7]), max(q[1], q[3], q[5], q[7])
		minX, minY = max(minX, 0), max(minY, 0)
		maxX, maxY = min(maxX, vp.Width), min(maxY, vp.Height)
		if maxX-minX >= 1 && maxY-minY >= 1 {
			return (minX + maxX) / 2, (minY + maxY) / 2, vp, nil
		}
	}
	return 0, 0, vp, &Failure{Code: CodeNotVisible, Ref: ref, Detail: fmt.Sprintf("%s has no visible area in the viewport", ref)}
}

// reachesTarget runs on the target with the hit as its argument. The hit
// reaches it from inside it, or from inside one of its labels with no other
// interactive element between: a link in a label follows the link. Newer
// Chromium resolves a pseudo-element hit to a CSSPseudoElement, which is not
// a node and names its element as .element.
const reachesTarget = `function(hit){
	const labels = Array.from(this.labels || []);
	let through = false;
	for (let n = hit; n; n = n.parentNode || n.host || (n.nodeType === undefined ? n.element : null)) {
		if (n === this) return true;
		if (labels.includes(n)) return !through;
		if (n.nodeType === 1 && n.matches("a[href],button,input,select,textarea,summary,iframe,embed,label")) through = true;
	}
	return false;
}`

// checkHit refuses an input whose point, in viewport coordinates, would land
// on an element that does not reach node, such as a banner drawn over it.
func (s *Session) checkHit(ctx context.Context, t *tab, node int64, ref string, x, y float64, vp viewport) error {
	var hit struct {
		BackendNodeID int64 `json:"backendNodeId"`
	}
	if err := t.call(ctx, "DOM.getNodeForLocation", map[string]any{"x": int(x + vp.PageX), "y": int(y + vp.PageY), "includeUserAgentShadowDOM": false}, &hit); err != nil {
		if isProtocolError(err) {
			return nil
		}
		return engineFailure(err)
	}
	if hit.BackendNodeID == 0 || hit.BackendNodeID == node {
		return nil
	}
	targetObj, err := t.resolveObject(ctx, node)
	if err != nil {
		return err
	}
	hitObj, err := t.resolveObject(ctx, hit.BackendNodeID)
	if err != nil {
		return err
	}
	var within struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	if err := t.call(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId":            targetObj,
		"functionDeclaration": reachesTarget,
		"arguments":           []map[string]any{{"objectId": hitObj}},
		"returnByValue":       true,
	}, &within); err != nil {
		return engineFailure(err)
	}
	if within.Result.Value {
		return nil
	}
	cover := s.refs.refFor(t.id, hit.BackendNodeID)
	return &Failure{Code: CodeCovered, Ref: ref, Detail: fmt.Sprintf("%s is under %s %s, which a click on it would reach instead, even with %s scrolled to the middle of the page; act on %s if that is what you meant, or close or move it first", ref, t.describe(ctx, hit.BackendNodeID), cover, ref, cover)}
}

// labels are the backend nodes of the <label> elements that activate node.
// The list comes from the page, so its length and kinds are checked here.
func (t *tab) labels(ctx context.Context, node int64) ([]int64, error) {
	obj, err := t.resolveObject(ctx, node)
	if err != nil {
		return nil, err
	}
	var list struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := t.call(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId":            obj,
		"functionDeclaration": fmt.Sprintf("function(){return Array.from(this.labels || []).slice(0, %d)}", maxLabels),
	}, &list); err != nil {
		return nil, engineFailure(err)
	}
	if list.Result.ObjectID == "" {
		return nil, nil
	}
	var props struct {
		Result []struct {
			Name  string `json:"name"`
			Value struct {
				ObjectID string `json:"objectId"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := t.call(ctx, "Runtime.getProperties", map[string]any{"objectId": list.Result.ObjectID, "ownProperties": true}, &props); err != nil {
		return nil, engineFailure(err)
	}
	var out []int64
	for _, p := range props.Result {
		if len(out) == maxLabels {
			break
		}
		if _, err := strconv.Atoi(p.Name); err != nil || p.Value.ObjectID == "" {
			continue
		}
		var d struct {
			Node struct {
				BackendNodeID int64  `json:"backendNodeId"`
				LocalName     string `json:"localName"`
			} `json:"node"`
		}
		if err := t.call(ctx, "DOM.describeNode", map[string]any{"objectId": p.Value.ObjectID}, &d); err != nil {
			if isProtocolError(err) {
				continue
			}
			return nil, engineFailure(err)
		}
		if d.Node.BackendNodeID != 0 && d.Node.LocalName == "label" {
			out = append(out, d.Node.BackendNodeID)
		}
	}
	return out, nil
}
