package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

const navigationTimeout = 30 * time.Second

// Config is what a Session needs from its host. Launch.Executable is the
// configured browser, or empty to find one when the browser is first needed;
// Roots bound which local files a page may be.
type Config struct {
	Launch LaunchSpec
	Roots  []string
	Pool   *Pool
}

// Session is one agent's view of the browser: the tabs it opened and the refs
// it has been shown. Its browser starts on first use, is shared with other
// sessions on the same profile, and is released by Close.
type Session struct {
	cfg Config

	mu       sync.Mutex
	eng      *engine
	stopEng  func()
	tabs     []*tab
	active   *tab
	nextTab  int
	refs     refTable
	closed   bool
	owners   int
	openedBy map[string]string // popup target id → opener tab id
	lost     map[string]bool   // tabs closed outside the agent since it last opened one
	changed  func()
	checks   browserCallChecks
}

type browserCallChecks struct {
	secrets      map[string]bool
	scriptOrigin map[string]string
}

func NewSession(cfg Config) *Session {
	if cfg.Pool == nil {
		cfg.Pool = &Pool{}
	}
	return &Session{cfg: cfg, openedBy: map[string]string{}, lost: map[string]bool{}}
}

// TabInfo is what a tab is showing. Target is the browser's own id for the
// page, which a window drawing hosted views keys them by.
type TabInfo struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// ensureEngine returns a live browser, starting one when this session has none
// or the one it had is gone. Tabs on a browser that went away go with it.
func (s *Session) ensureEngine(ctx context.Context) (*engine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fail(CodeEngineFailed, "the browser session is closed")
	}
	if s.eng != nil && s.eng.alive() {
		return s.eng, nil
	}
	if s.eng != nil {
		s.dropEngineLocked()
	}
	eng, err := s.cfg.Pool.acquire(ctx, s.cfg.Launch)
	if err != nil {
		return nil, err
	}
	s.eng = eng
	s.stopEng = eng.listen(s.onBrowserEvent)
	return eng, nil
}

func (s *Session) dropEngineLocked() {
	if s.stopEng != nil {
		s.stopEng()
		s.stopEng = nil
	}
	for _, t := range s.tabs {
		t.detach()
		s.lost[t.id] = true
	}
	s.tabs, s.active = nil, nil
	eng := s.eng
	s.eng = nil
	go s.cfg.Pool.release(eng)
}

// OnTabsChanged names who hears that a tab opened, closed, became active or
// navigated. It replaces whoever heard before.
func (s *Session) OnTabsChanged(fn func()) {
	s.mu.Lock()
	s.changed = fn
	s.mu.Unlock()
}

func (s *Session) tabsChanged() {
	s.mu.Lock()
	fn := s.changed
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Retain adds an owner. A session outlives a rebuild by being retained by the
// runtime that replaces its owner before that owner releases it.
func (s *Session) Retain() {
	s.mu.Lock()
	s.owners++
	s.mu.Unlock()
}

// Release drops an owner, and closes the session when it was the last one.
func (s *Session) Release() {
	s.mu.Lock()
	if s.owners > 0 {
		s.owners--
	}
	last := s.owners == 0
	s.mu.Unlock()
	if last {
		s.Close()
	}
}

// Close closes this session's tabs and releases its browser.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	eng, tabs := s.eng, s.tabs
	s.mu.Unlock()
	if eng == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	for _, t := range tabs {
		_ = eng.conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID}, nil)
		t.detach()
	}
	s.mu.Lock()
	if s.stopEng != nil {
		s.stopEng()
	}
	s.eng = nil
	s.mu.Unlock()
	s.cfg.Pool.release(eng)
}

// Origin is the scheme, host and port of the page a tab shows ("" is the
// active tab), or "" when it shows no page. A local file's origin is file://.
func (s *Session) Origin(tabID string) string {
	t, err := s.tab(tabID)
	if err != nil {
		return ""
	}
	raw, _ := t.location()
	return OriginOf(raw)
}

// PageURL is the address a tab is on, or "" when there is no such tab. The
// host's answer, not the model's: a step may have navigated away from what it
// asked for.
func (s *Session) PageURL(tabID string) string {
	t, err := s.tab(tabID)
	if err != nil {
		return ""
	}
	raw, _ := t.location()
	return raw
}

// Tabs lists this session's tabs in the order they opened.
func (s *Session) Tabs() []TabInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TabInfo, 0, len(s.tabs))
	for _, t := range s.tabs {
		url, title := t.location()
		out = append(out, TabInfo{ID: t.id, Target: t.targetID, URL: url, Title: title, Active: t == s.active})
	}
	return out
}

// tab resolves a tab id, "" meaning the active tab.
func (s *Session) tab(id string) (*tab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		if s.active == nil {
			if len(s.lost) > 0 {
				return nil, closedOutside("the page")
			}
			return nil, fail(CodeNoTab, "no page is open; open one first")
		}
		return s.active, nil
	}
	for _, t := range s.tabs {
		if t.id == id {
			return t, nil
		}
	}
	if s.lost[id] {
		return nil, closedOutside("tab " + id)
	}
	return nil, fail(CodeNoTab, "no tab %s in this session", id)
}

// Open navigates a tab to rawURL and waits for it to load. newTab opens a fresh
// tab; otherwise the named tab, or the active one, navigates.
func (s *Session) Open(ctx context.Context, rawURL, tabID string, newTab bool) (TabInfo, error) {
	return s.open(ctx, rawURL, tabID, newTab, true)
}

// Visit is Open for a person watching the page: it returns once the page has
// committed rather than when every subresource has loaded. An intranet page
// referencing a host it cannot reach never fires load, and the person already
// sees it; the agent's Open still waits, because it reads what loaded.
func (s *Session) Visit(ctx context.Context, rawURL, tabID string, newTab bool) (TabInfo, error) {
	return s.open(ctx, rawURL, tabID, newTab, false)
}

func (s *Session) open(ctx context.Context, rawURL, tabID string, newTab, untilLoaded bool) (TabInfo, error) {
	target, err := checkURL(rawURL, s.cfg.Roots)
	if err != nil {
		return TabInfo{}, err
	}
	eng, err := s.ensureEngine(ctx)
	if err != nil {
		return TabInfo{}, err
	}
	var t *tab
	if newTab || (tabID == "" && s.activeTab() == nil) {
		t, err = s.createTab(ctx, eng)
	} else {
		t, err = s.tab(tabID)
	}
	if err != nil {
		return TabInfo{}, err
	}
	s.activate(t)
	if untilLoaded {
		err = t.navigate(ctx, target)
	} else {
		startCtx, cancel := context.WithTimeout(ctx, navigationTimeout)
		_, err = t.start(startCtx, target)
		cancel()
	}
	if err != nil {
		return t.info(true), err
	}
	s.mu.Lock()
	clear(s.lost)
	s.mu.Unlock()
	return t.info(true), nil
}

// closedOutside names a page the browser lost without the agent closing it:
// the person closed it, the page closed itself, or the browser went away.
func closedOutside(what string) *Failure {
	return fail(CodeTabClosed, "%s was closed outside the agent (by the person, by the page itself, or because the browser exited); ask whether to open it again rather than reopening it", what)
}

func (s *Session) activeTab() *tab {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *Session) activate(t *tab) {
	s.mu.Lock()
	s.active = t
	s.mu.Unlock()
	defer s.tabsChanged()
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	_ = t.call(ctx, "Page.bringToFront", nil, nil)
}

// Switch makes a tab the active one.
func (s *Session) Switch(tabID string) (TabInfo, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return TabInfo{}, err
	}
	s.activate(t)
	return t.info(true), nil
}

// CloseTab closes one tab; the most recently opened remaining tab becomes active.
func (s *Session) CloseTab(ctx context.Context, tabID string) error {
	t, err := s.tab(tabID)
	if err != nil {
		return err
	}
	// Removed first, so the destruction it causes is not read as someone else's.
	s.removeTab(t)
	_ = t.eng.conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID}, nil)
	return nil
}

func (s *Session) createTab(ctx context.Context, eng *engine) (*tab, error) {
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := eng.conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return nil, engineFailure(err)
	}
	return s.attach(ctx, eng, created.TargetID)
}

func (s *Session) attach(ctx context.Context, eng *engine, targetID string) (*tab, error) {
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := eng.conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true}, &attached); err != nil {
		return nil, engineFailure(err)
	}
	s.mu.Lock()
	s.nextTab++
	t := newTab(s, eng, fmt.Sprintf("t%d", s.nextTab), targetID, attached.SessionID)
	s.tabs = append(s.tabs, t)
	s.mu.Unlock()
	if err := t.enable(ctx); err != nil {
		s.removeTab(t)
		return nil, engineFailure(err)
	}
	s.tabsChanged()
	return t, nil
}

func (s *Session) removeTab(t *tab) {
	t.detach()
	defer s.tabsChanged()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, candidate := range s.tabs {
		if candidate == t {
			s.tabs = append(s.tabs[:i], s.tabs[i+1:]...)
			break
		}
	}
	if s.active == t {
		s.active = nil
		if n := len(s.tabs); n > 0 {
			s.active = s.tabs[n-1]
		}
	}
	s.refs.retireTab(t.id)
}

// onBrowserEvent follows tabs this session did not open itself: a page one of
// its tabs opened, and any of its tabs closing.
func (s *Session) onBrowserEvent(ev event) {
	switch ev.Method {
	case "Target.targetCreated":
		var p struct {
			TargetInfo struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				OpenerID string `json:"openerId"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(ev.Params, &p) != nil || p.TargetInfo.Type != "page" || p.TargetInfo.OpenerID == "" {
			return
		}
		opener := s.tabByTarget(p.TargetInfo.OpenerID)
		if opener == nil {
			return
		}
		s.mu.Lock()
		eng := s.eng
		s.mu.Unlock()
		if eng == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), launchTimeout)
		defer cancel()
		if popup, err := s.attach(ctx, eng, p.TargetInfo.TargetID); err == nil {
			s.mu.Lock()
			s.openedBy[popup.targetID] = opener.id
			s.active = popup
			s.mu.Unlock()
			opener.notePopup(popup.id)
		}
	case "Browser.downloadWillBegin":
		var p struct {
			FrameID           string `json:"frameId"`
			URL               string `json:"url"`
			SuggestedFilename string `json:"suggestedFilename"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		if t := s.tabByFrame(p.FrameID); t != nil {
			t.log("download", "error", fmt.Sprintf("download of %q from %s was refused: the agent's browser does not download files", p.SuggestedFilename, p.URL))
		}
	case "Target.targetDestroyed":
		var p struct {
			TargetID string `json:"targetId"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		if t := s.tabByTarget(p.TargetID); t != nil {
			s.mu.Lock()
			s.lost[t.id] = true
			s.mu.Unlock()
			s.removeTab(t)
		}
	}
}

func (s *Session) tabByFrame(frameID string) *tab {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tabs {
		t.mu.Lock()
		main := t.mainFrame
		t.mu.Unlock()
		if main == frameID {
			return t
		}
	}
	return nil
}

func (s *Session) tabByTarget(targetID string) *tab {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tabs {
		if t.targetID == targetID {
			return t
		}
	}
	return nil
}

// engineFailure attributes an error from the connection itself. A protocol
// error is the browser refusing a command and stays as it is.
func engineFailure(err error) error {
	switch {
	case err == nil:
		return nil
	case CodeOf(err) != "":
		return err
	case errors.Is(err, errConnClosed):
		return fail(CodeEngineFailed, "the browser closed; open a page again to restart it")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	default:
		return fail(CodeEngineFailed, "%v", err)
	}
}
