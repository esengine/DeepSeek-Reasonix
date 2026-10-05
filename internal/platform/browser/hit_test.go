package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestHitTestAsksForTheDocumentPointUnderTheViewportPoint(t *testing.T) {
	var asked map[string]any
	var clicked []float64
	page := &fakePage{answers: func(msg wireMessage) (any, bool) {
		switch msg.Method {
		case "Page.getLayoutMetrics":
			return map[string]any{"cssLayoutViewport": map[string]any{"clientWidth": 800, "clientHeight": 600, "pageX": 30, "pageY": 1000}}, true
		case "DOM.getContentQuads":
			return map[string]any{"quads": [][]float64{{10, 40, 110, 40, 110, 80, 10, 80}}}, true
		case "DOM.getNodeForLocation":
			_ = json.Unmarshal(msg.Params, &asked)
			return map[string]any{"backendNodeId": 7}, true
		case "Input.dispatchMouseEvent":
			var p struct {
				Type string  `json:"type"`
				X    float64 `json:"x"`
				Y    float64 `json:"y"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if p.Type == "mousePressed" {
				clicked = []float64{p.X, p.Y}
			}
		}
		return nil, false
	}}
	s, _ := newFakeSession(t, page)
	ref := s.refs.refFor("t1", 7)
	if _, err := s.Act(context.Background(), "", []Step{{Action: "click", Ref: ref}}, false, ""); err != nil {
		t.Fatalf("Act: %v", err)
	}
	if asked["x"] != float64(90) || asked["y"] != float64(1060) {
		t.Fatalf("hit test asked at (%v, %v), want the document point (90, 1060) of viewport point (60, 60)", asked["x"], asked["y"])
	}
	if v, ok := asked["ignorePointerEventsNone"]; ok && v != false {
		t.Fatalf("hit test counted pointer-events:none elements as hits: %v", asked)
	}
	if len(clicked) != 2 || clicked[0] != 60 || clicked[1] != 60 {
		t.Fatalf("clicked at %v, want the viewport point (60, 60)", clicked)
	}
}

const activate = `document.title='hit'`

// reachCases are pages where the element a ref names is or is not what a
// person clicking it would activate. reaches says which.
var reachCases = []struct {
	name, needle, page string
	open               bool // click "Open" in the same call first
	reaches            bool
}{
	{name: "zero-size checkbox drawn by its label", needle: `checkbox "Agree"`, reaches: true, page: `<label><span style="display:inline-block;position:relative;width:14px;height:14px;border:1px solid"><input type="checkbox" onchange="` + activate + `" style="opacity:0;position:absolute;margin:0;width:0;height:0;z-index:-1"></span><span> Agree</span></label>`},
	{name: "visually hidden checkbox with a label elsewhere", needle: `checkbox "Agree"`, reaches: true, page: `<input type="checkbox" id="c" onchange="` + activate + `" style="position:absolute;width:1px;height:1px;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);border:0"><label for="c" style="display:inline-block;padding:8px 20px">Agree</label>`},
	{name: "radio drawn over by a span in its label", needle: `radio "Yes"`, reaches: true, page: `<label style="position:relative;display:inline-block"><input type="radio" onchange="` + activate + `" style="margin:0;width:16px;height:16px"><span style="position:absolute;left:0;top:0;width:16px;height:16px;background:#39f"></span> Yes</label>`},
	{name: "pseudo-element over a button", needle: `button "Go"`, reaches: true, page: `<style>.r{position:relative;padding:10px 30px}.r::after{content:"";position:absolute;inset:0}</style><button class="r" onclick="` + activate + `">Go</button>`},
	{name: "pointer-events:none overlay", needle: `button "Under"`, reaches: true, page: `<div style="position:relative"><button onclick="` + activate + `" style="padding:20px">Under</button><div style="position:absolute;inset:0;background:rgba(255,0,0,.2);pointer-events:none"></div></div>`},
	{name: "page scrolled with a banner above", needle: `button "Buy"`, reaches: true, page: `<body style="margin:0"><div class="banner" style="height:420px">Banner</div><div style="height:80px"></div><button onclick="` + activate + `" style="height:40px">Buy</button><div style="height:3000px"></div><script>addEventListener('load',()=>scrollTo(0,250))</script></body>`},
	{name: "fixed header over the target", needle: `button "Target"`, reaches: true, page: `<body style="margin:0"><header style="position:fixed;top:0;left:0;right:0;height:90px;background:#333;z-index:10">Header</header><div style="height:1000px"></div><button id="t" onclick="` + activate + `" style="height:30px">Target</button><div style="height:2000px"></div><script>addEventListener('load',()=>scrollTo(0,document.getElementById('t').offsetTop-40))</script></body>`},
	{name: "smooth scrolling page", needle: `button "Far"`, reaches: true, page: `<style>html{scroll-behavior:smooth}</style><div style="height:5000px"></div><button onclick="` + activate + `">Far</button><div style="height:3000px"></div>`},
	{name: "menu sliding in", needle: `button "Item"`, open: true, reaches: true, page: `<style>#m{position:fixed;left:0;top:60px;width:260px;transform:translateX(-270px);transition:transform .6s}#m.on{transform:none}</style><button onclick="document.getElementById('m').classList.add('on')">Open</button><nav id="m"><button onclick="` + activate + `" style="width:240px;height:40px">Item</button></nav>`},
	{name: "closed shadow root", needle: `button "Shadow"`, reaches: true, page: `<div id="h"></div><script>const r=document.getElementById('h').attachShadow({mode:'closed'});r.innerHTML='<button><span>Shadow</span></button>';r.querySelector('button').onclick=()=>{` + activate + `}</script>`},
	{name: "modal backdrop", needle: `button "Behind"`, page: `<button onclick="` + activate + `">Behind</button><div class="backdrop" style="position:fixed;inset:0;background:rgba(0,0,0,.4)"></div>`},
	{name: "veil on a scrolled page", needle: `button "Deep"`, page: `<body style="margin:0"><div style="height:2300px"></div><div style="position:relative;height:40px"><button onclick="` + activate + `">Deep</button><div class="veil" style="position:absolute;inset:0;background:#fff"></div></div><div style="height:3000px"></div><script>addEventListener('load',()=>scrollTo(0,2000))</script></body>`},
	{name: "label of another control on top", needle: `checkbox "A"`, page: `<div style="position:relative"><input type="checkbox" id="a" onchange="` + activate + `"><label for="a">A</label><label for="b" style="position:absolute;left:0;top:0;width:40px;height:30px;background:#fff">B</label><input type="checkbox" id="b"></div>`},
	{name: "link inside the label where the control is hidden", needle: `checkbox "terms"`, page: `<label><input type="checkbox" onchange="` + activate + `" style="opacity:0;position:absolute;width:0;height:0"><a href="#terms" onclick="document.title='link';return false" style="display:inline-block;padding:10px 40px">terms</a></label>`},
}

func TestLiveAClickReachesWhatAPersonWouldClick(t *testing.T) {
	s := liveSession(t, true)
	pages := map[string]string{}
	for i, c := range reachCases {
		pages[fmt.Sprintf("/c%d", i)] = `<!doctype html><title>start</title>` + c.page
	}
	srv := liveServer(t, pages)
	for _, scale := range []float64{1, 1.5} {
		for i, c := range reachCases {
			t.Run(fmt.Sprintf("%s at %vx", c.name, scale), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
				defer cancel()
				if _, err := s.Open(ctx, fmt.Sprintf("%s/c%d", srv.URL, i), "", false); err != nil {
					t.Fatalf("Open: %v", err)
				}
				if err := s.activeTab().call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": 1200, "height": 800, "deviceScaleFactor": scale, "mobile": false}, nil); err != nil {
					t.Fatalf("scale: %v", err)
				}
				snap, err := s.Snapshot(ctx, "", "")
				if err != nil {
					t.Fatalf("Snapshot: %v", err)
				}
				steps := []Step{{Action: "click", Ref: findRef(t, snap, c.needle)}}
				if c.open {
					steps = append([]Step{{Action: "click", Ref: findRef(t, snap, `button "Open"`)}}, steps...)
				}
				res, err := actPlain(ctx, s, "", steps)
				switch {
				case c.reaches && (err != nil || res.Tab.Title != "hit"):
					t.Fatalf("click = %v, title %q; want it to reach the element", err, res.Tab.Title)
				case !c.reaches && (CodeOf(err) != CodeCovered || res.Tab.Title != "start"):
					t.Fatalf("click = %v, title %q; want %s and nothing clicked", err, res.Tab.Title, CodeCovered)
				case !c.reaches && !strings.Contains(err.Error(), " e"):
					t.Fatalf("the refusal does not name what is on top by ref: %v", err)
				}
			})
		}
	}
}
