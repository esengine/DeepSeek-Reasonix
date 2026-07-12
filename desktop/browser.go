package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdpinput "github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"reasonix/internal/config"
	"reasonix/internal/secrets"
)

const (
	browserExecutableEnv  = "REASONIX_CHROMIUM_PATH"
	browserDefaultWidth   = 1280
	browserDefaultHeight  = 800
	browserCommandTimeout = 30 * time.Second

	browserFrameEvent = "browser:frame"
	browserStateEvent = "browser:state"
	browserExitEvent  = "browser:exit"
)

type BrowserFrameMetadata struct {
	OffsetTop     float64 `json:"offsetTop"`
	PageScale     float64 `json:"pageScale"`
	DeviceWidth   float64 `json:"deviceWidth"`
	DeviceHeight  float64 `json:"deviceHeight"`
	ScrollOffsetX float64 `json:"scrollOffsetX"`
	ScrollOffsetY float64 `json:"scrollOffsetY"`
}

type BrowserFramePayload struct {
	TabID    string               `json:"tabId"`
	PageID   string               `json:"pageId"`
	Sequence uint64               `json:"sequence"`
	Data     string               `json:"data"`
	Metadata BrowserFrameMetadata `json:"metadata"`
}

type browserExitPayload struct {
	TabID    string `json:"tabId"`
	PageID   string `json:"pageId"`
	Error    string `json:"error,omitempty"`
	Expected bool   `json:"expected"`
}

type BrowserMouseEvent struct {
	Type       string  `json:"type"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button,omitempty"`
	Buttons    int64   `json:"buttons,omitempty"`
	ClickCount int64   `json:"clickCount,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
	Modifiers  int64   `json:"modifiers,omitempty"`
}

type BrowserKeyEvent struct {
	Type                  string `json:"type"`
	Key                   string `json:"key,omitempty"`
	Code                  string `json:"code,omitempty"`
	Text                  string `json:"text,omitempty"`
	UnmodifiedText        string `json:"unmodifiedText,omitempty"`
	WindowsVirtualKeyCode int64  `json:"windowsVirtualKeyCode,omitempty"`
	NativeVirtualKeyCode  int64  `json:"nativeVirtualKeyCode,omitempty"`
	Modifiers             int64  `json:"modifiers,omitempty"`
	AutoRepeat            bool   `json:"autoRepeat,omitempty"`
	IsKeypad              bool   `json:"isKeypad,omitempty"`
}

type BrowserRuntimeInfo struct {
	Available      bool   `json:"available"`
	ExecutablePath string `json:"executablePath,omitempty"`
	ProfilePath    string `json:"profilePath,omitempty"`
	Error          string `json:"error,omitempty"`
}

type BrowserSessionView struct {
	TabID        string `json:"tabId"`
	PageID       string `json:"pageId"`
	URL          string `json:"url"`
	Title        string `json:"title,omitempty"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	CanAnnotate  bool   `json:"canAnnotate"`
	Sequence     uint64 `json:"sequence"`
}

type browserManager struct {
	app *App

	mu              sync.Mutex
	sessions        map[string]*browserSession
	allocatorCancel context.CancelFunc
	browserCancel   context.CancelFunc
	browserContext  context.Context
	executablePath  string
	profilePath     string
	browser         *chromedp.Browser
	generation      uint64
	popupTargets    map[target.ID]string
	popupHandled    map[target.ID]bool
	nextID          atomic.Uint64
}

type browserSession struct {
	manager    *browserManager
	tabID      string
	ctx        context.Context
	cancel     context.CancelFunc
	generation uint64
	ready      chan struct{}
	readyOnce  sync.Once
	startMu    sync.Mutex
	startErr   error

	opMu    sync.Mutex
	stateMu sync.RWMutex
	state   BrowserSessionView
	closed  atomic.Bool

	frameMu              sync.Mutex
	frameEmitMu          sync.Mutex
	screencastStarted    bool
	frameSequence        uint64
	pendingFrameSequence uint64
	pendingCDPSessionID  int64

	inspectorMu       sync.Mutex
	inspector         *browserInspectorState
	inspectorSequence uint64
}

func newBrowserManager(app *App) *browserManager {
	return &browserManager{
		app:          app,
		sessions:     map[string]*browserSession{},
		popupTargets: map[target.ID]string{},
		popupHandled: map[target.ID]bool{},
	}
}

func (a *App) browserManager() *browserManager {
	if a.browsers == nil {
		a.browsers = newBrowserManager(a)
	}
	return a.browsers
}

func (a *App) BrowserRuntimeInfo() BrowserRuntimeInfo {
	return a.browserManager().runtimeInfo()
}

func (a *App) BrowserOpen(tabID, rawURL string, width, height int) (BrowserSessionView, error) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return BrowserSessionView{}, errors.New("browser tab id is required")
	}
	if _, err := a.workspaceBaseForTab(tabID); err != nil {
		return BrowserSessionView{}, err
	}
	return a.browserManager().open(tabID, rawURL, width, height)
}

func (a *App) BrowserClose(tabID, pageID string) error {
	return a.browserManager().close(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
}

func (a *App) BrowserStartScreencast(tabID, pageID string) error {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return err
	}
	return session.startScreencast()
}

func (a *App) BrowserFrameAck(tabID, pageID string, sequence uint64) error {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return err
	}
	return session.ackFrame(sequence)
}

func (a *App) BrowserNavigate(tabID, pageID, rawURL string) (BrowserSessionView, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserSessionView{}, err
	}
	normalizedURL, err := normalizeBrowserURL(rawURL)
	if err != nil {
		return BrowserSessionView{}, err
	}
	return session.navigate(normalizedURL)
}

func (a *App) BrowserReload(tabID, pageID string) (BrowserSessionView, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserSessionView{}, err
	}
	return session.reload()
}

func (a *App) BrowserGoBack(tabID, pageID string) (BrowserSessionView, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserSessionView{}, err
	}
	return session.navigateHistory(-1)
}

func (a *App) BrowserGoForward(tabID, pageID string) (BrowserSessionView, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserSessionView{}, err
	}
	return session.navigateHistory(1)
}

func (a *App) BrowserResize(tabID, pageID string, width, height int) (BrowserSessionView, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserSessionView{}, err
	}
	return session.resize(width, height)
}

func (a *App) BrowserMouse(tabID, pageID string, event BrowserMouseEvent) error {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return err
	}
	return session.mouse(event)
}

func (a *App) BrowserKey(tabID, pageID string, event BrowserKeyEvent) error {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return err
	}
	return session.key(event)
}

func (a *App) BrowserInsertText(tabID, pageID, text string) error {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return err
	}
	return session.insertText(text)
}

func (m *browserManager) runtimeInfo() BrowserRuntimeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.executablePath != "" {
		return BrowserRuntimeInfo{
			Available:      true,
			ExecutablePath: m.executablePath,
			ProfilePath:    m.profilePath,
		}
	}
	executablePath, err := resolveChromiumExecutable()
	profilePath, profileErr := browserProfilePath()
	if err == nil && profileErr == nil {
		return BrowserRuntimeInfo{
			Available:      true,
			ExecutablePath: executablePath,
			ProfilePath:    profilePath,
		}
	}
	if err == nil {
		err = profileErr
	}
	return BrowserRuntimeInfo{Error: err.Error()}
}

func (m *browserManager) open(tabID, rawURL string, width, height int) (BrowserSessionView, error) {
	normalizedURL, err := normalizeBrowserURL(rawURL)
	if err != nil {
		return BrowserSessionView{}, err
	}
	width, height = clampBrowserViewport(width, height)

	m.mu.Lock()
	if current := m.sessions[tabID]; current != nil && !current.closed.Load() {
		m.mu.Unlock()
		return current.waitReady()
	}
	if err := m.ensureBrowserLocked(); err != nil {
		m.mu.Unlock()
		return BrowserSessionView{}, err
	}
	ctx, cancel := chromedp.NewContext(m.browserContext)
	session := &browserSession{
		manager:    m,
		tabID:      tabID,
		ctx:        ctx,
		cancel:     cancel,
		generation: m.generation,
		ready:      make(chan struct{}),
		state: BrowserSessionView{
			TabID:       tabID,
			URL:         normalizedURL,
			Width:       width,
			Height:      height,
			CanAnnotate: isLocalBrowserURL(normalizedURL),
			Sequence:    1,
		},
	}
	m.sessions[tabID] = session
	session.listen()
	m.mu.Unlock()

	if err := chromedp.Run(ctx); err != nil {
		err = fmt.Errorf("create browser page: %w", err)
		session.failStart(err)
		return BrowserSessionView{}, err
	}
	chromedpContext := chromedp.FromContext(ctx)
	if chromedpContext == nil || chromedpContext.Target == nil {
		err = errors.New("browser page target was not created")
		session.failStart(err)
		return BrowserSessionView{}, err
	}
	pageID := string(chromedpContext.Target.TargetID)
	if pageID == "" {
		pageID = fmt.Sprintf("browser-%d-%d", time.Now().UnixMilli(), m.nextID.Add(1))
	}
	session.setPageID(pageID)
	if err := session.runCommand(func(commandContext context.Context) error {
		if err := chromedp.EmulateViewport(int64(width), int64(height)).Do(commandContext); err != nil {
			return err
		}
		if err := chromedp.Navigate(normalizedURL).Do(commandContext); err != nil {
			return err
		}
		return session.refreshStateLocked(commandContext)
	}); err != nil {
		err = fmt.Errorf("open browser page: %w", err)
		session.failStart(err)
		return BrowserSessionView{}, err
	}

	m.mu.Lock()
	current := m.sessions[tabID]
	valid := current == session && !session.closed.Load() && session.generation == m.generation
	m.mu.Unlock()
	if !valid {
		err = errors.New("browser page was closed while starting")
		session.failStart(err)
		return BrowserSessionView{}, err
	}
	session.completeStart(nil)
	return session.waitReady()
}

func (m *browserManager) ensureBrowserLocked() error {
	if m.browserContext != nil {
		return nil
	}
	executablePath, err := resolveChromiumExecutable()
	if err != nil {
		return err
	}
	profilePath, err := browserProfilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(profilePath, 0o700); err != nil {
		return fmt.Errorf("create Reasonix browser profile: %w", err)
	}

	options := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(executablePath),
		chromedp.UserDataDir(profilePath),
		chromedp.Env(secrets.FilterEnv(os.Environ())...),
		chromedp.Headless,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("enable-automation", true),
		chromedp.Flag("remote-debugging-address", "127.0.0.1"),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-extensions", true),
	}
	allocatorContext, allocatorCancel := chromedp.NewExecAllocator(m.app.bootContext(), options...)
	browserContext, browserCancel := chromedp.NewContext(allocatorContext)
	if err := chromedp.Run(browserContext); err != nil {
		browserCancel()
		allocatorCancel()
		return fmt.Errorf("start bundled Chromium: %w", err)
	}
	chromedpContext := chromedp.FromContext(browserContext)
	if chromedpContext == nil || chromedpContext.Browser == nil {
		browserCancel()
		allocatorCancel()
		return errors.New("start bundled Chromium: browser connection was not created")
	}
	browser := chromedpContext.Browser
	if err := target.SetDiscoverTargets(true).Do(cdp.WithExecutor(browserContext, browser)); err != nil {
		browserCancel()
		allocatorCancel()
		return fmt.Errorf("start bundled Chromium target discovery: %w", err)
	}

	m.generation++
	generation := m.generation
	m.executablePath = executablePath
	m.profilePath = profilePath
	m.allocatorCancel = allocatorCancel
	m.browserCancel = browserCancel
	m.browserContext = browserContext
	m.browser = browser
	m.popupTargets = map[target.ID]string{}
	m.popupHandled = map[target.ID]bool{}
	chromedp.ListenBrowser(browserContext, func(event any) {
		m.handleBrowserEvent(generation, event)
	})
	go m.watchBrowser(generation, browserContext, browser.LostConnection)
	return nil
}

func (m *browserManager) session(tabID, pageID string) (*browserSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[tabID]
	if session == nil || session.closed.Load() {
		return nil, errors.New("browser session is not running")
	}
	if pageID != "" && session.pageID() != pageID {
		return nil, errors.New("browser page changed")
	}
	return session, nil
}

func (m *browserManager) close(tabID, pageID string) error {
	if tabID == "" {
		return nil
	}
	m.mu.Lock()
	session := m.sessions[tabID]
	if session == nil || (pageID != "" && session.pageID() != pageID) {
		m.mu.Unlock()
		return nil
	}
	delete(m.sessions, tabID)
	popupIDs := m.removePopupTargetsLocked(tabID)
	browserContext := m.browserContext
	browser := m.browser
	m.mu.Unlock()
	for _, popupID := range popupIDs {
		m.closePopupTarget(browserContext, browser, popupID)
	}
	session.stop()
	return nil
}

func (m *browserManager) closeForTab(tabID string) {
	tabID = strings.TrimSpace(tabID)
	if tabID == "" {
		return
	}
	m.mu.Lock()
	session := m.sessions[tabID]
	if session != nil {
		delete(m.sessions, tabID)
	}
	popupIDs := m.removePopupTargetsLocked(tabID)
	browserContext := m.browserContext
	browser := m.browser
	m.mu.Unlock()

	for _, popupID := range popupIDs {
		m.closePopupTarget(browserContext, browser, popupID)
	}
	if session != nil {
		session.stop()
	}
}

func (m *browserManager) shutdown() {
	m.mu.Lock()
	sessions := make([]*browserSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	clear(m.sessions)
	popupIDs := make([]target.ID, 0, len(m.popupTargets))
	for popupID := range m.popupTargets {
		popupIDs = append(popupIDs, popupID)
	}
	browserContext := m.browserContext
	browser := m.browser
	browserCancel := m.browserCancel
	allocatorCancel := m.allocatorCancel
	m.generation++
	m.browserCancel = nil
	m.allocatorCancel = nil
	m.browserContext = nil
	m.browser = nil
	m.executablePath = ""
	m.profilePath = ""
	clear(m.popupTargets)
	clear(m.popupHandled)
	m.mu.Unlock()

	for _, popupID := range popupIDs {
		m.closePopupTarget(browserContext, browser, popupID)
	}
	for _, session := range sessions {
		session.stop()
	}
	if browserContext != nil {
		ctx, cancel := context.WithTimeout(browserContext, 5*time.Second)
		_ = chromedp.Cancel(ctx)
		cancel()
	}
	if browserCancel != nil {
		browserCancel()
	}
	if allocatorCancel != nil {
		allocatorCancel()
	}
}

func (m *browserManager) watchBrowser(generation uint64, browserContext context.Context, lost <-chan struct{}) {
	<-lost

	m.mu.Lock()
	if m.generation != generation || m.browserContext != browserContext {
		m.mu.Unlock()
		return
	}
	sessions := make([]*browserSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	clear(m.sessions)
	browserCancel := m.browserCancel
	allocatorCancel := m.allocatorCancel
	m.generation++
	m.browserContext = nil
	m.browser = nil
	m.browserCancel = nil
	m.allocatorCancel = nil
	m.executablePath = ""
	m.profilePath = ""
	clear(m.popupTargets)
	clear(m.popupHandled)
	m.mu.Unlock()

	for _, session := range sessions {
		session.finish(false, "Bundled Chromium exited unexpectedly")
	}
	if browserCancel != nil {
		browserCancel()
	}
	if allocatorCancel != nil {
		allocatorCancel()
	}
}

func (m *browserManager) handleBrowserEvent(generation uint64, event any) {
	switch value := event.(type) {
	case *target.EventTargetCreated:
		m.observePopup(generation, value.TargetInfo)
	case *target.EventTargetInfoChanged:
		m.observePopup(generation, value.TargetInfo)
	case *target.EventTargetDestroyed:
		m.mu.Lock()
		if generation == m.generation {
			delete(m.popupTargets, value.TargetID)
			delete(m.popupHandled, value.TargetID)
		}
		m.mu.Unlock()
	case *target.EventTargetCrashed:
		m.handleTargetCrash(generation, value)
	}
}

func (m *browserManager) observePopup(generation uint64, info *target.Info) {
	if info == nil || info.Type != "page" || info.OpenerID == "" {
		return
	}
	m.mu.Lock()
	if generation != m.generation || m.browserContext == nil {
		m.mu.Unlock()
		return
	}
	tabID := m.popupOwnerLocked(info.OpenerID)
	if tabID == "" {
		m.mu.Unlock()
		return
	}
	m.popupTargets[info.TargetID] = tabID
	if m.popupHandled[info.TargetID] {
		m.mu.Unlock()
		return
	}
	rawURL := strings.TrimSpace(info.URL)
	if rawURL == "" || rawURL == "about:blank" {
		m.mu.Unlock()
		go m.closeBlankPopupAfter(generation, info.TargetID, 1500*time.Millisecond)
		return
	}
	m.popupHandled[info.TargetID] = true
	browserContext := m.browserContext
	browser := m.browser
	m.mu.Unlock()

	normalizedURL, err := normalizeBrowserURL(rawURL)
	go m.finishPopup(generation, info.TargetID, tabID, normalizedURL, err, browserContext, browser)
}

func (m *browserManager) closeBlankPopupAfter(generation uint64, popupID target.ID, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C

	m.mu.Lock()
	if generation != m.generation || m.popupHandled[popupID] {
		m.mu.Unlock()
		return
	}
	if _, ok := m.popupTargets[popupID]; !ok {
		m.mu.Unlock()
		return
	}
	m.popupHandled[popupID] = true
	browserContext := m.browserContext
	browser := m.browser
	m.mu.Unlock()
	m.closePopupTarget(browserContext, browser, popupID)
}

func (m *browserManager) finishPopup(generation uint64, popupID target.ID, tabID, normalizedURL string, normalizeErr error, browserContext context.Context, browser *chromedp.Browser) {
	m.closePopupTarget(browserContext, browser, popupID)
	if normalizeErr != nil || normalizedURL == "about:blank" {
		return
	}

	m.mu.Lock()
	session := m.sessions[tabID]
	valid := generation == m.generation && session != nil && !session.closed.Load()
	m.mu.Unlock()
	if valid {
		_, _ = session.navigate(normalizedURL)
	}
}

func (m *browserManager) handleTargetCrash(generation uint64, event *target.EventTargetCrashed) {
	if event == nil {
		return
	}
	m.mu.Lock()
	if generation != m.generation {
		m.mu.Unlock()
		return
	}
	var crashed *browserSession
	for _, session := range m.sessions {
		if target.ID(session.pageID()) == event.TargetID {
			crashed = session
			break
		}
	}
	m.mu.Unlock()
	if crashed != nil {
		message := fmt.Sprintf("Chromium page crashed (%s, code %d)", event.Status, event.ErrorCode)
		go crashed.finish(false, message)
	}
}

func (m *browserManager) popupOwnerLocked(openerID target.ID) string {
	if tabID := m.popupTargets[openerID]; tabID != "" {
		return tabID
	}
	for tabID, session := range m.sessions {
		if target.ID(session.pageID()) == openerID {
			return tabID
		}
	}
	return ""
}

func (m *browserManager) removePopupTargetsLocked(tabID string) []target.ID {
	popupIDs := make([]target.ID, 0)
	for popupID, ownerTabID := range m.popupTargets {
		if ownerTabID != tabID {
			continue
		}
		popupIDs = append(popupIDs, popupID)
		delete(m.popupTargets, popupID)
		delete(m.popupHandled, popupID)
	}
	return popupIDs
}

func (m *browserManager) closePopupTarget(browserContext context.Context, browser *chromedp.Browser, popupID target.ID) {
	if browserContext == nil || browser == nil || popupID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(browserContext, 5*time.Second)
	defer cancel()
	_ = target.CloseTarget(popupID).Do(cdp.WithExecutor(ctx, browser))
}

func sanitizeBrowserError(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.ReplaceAll(message, "\n", " ")
	runes := []rune(message)
	if len(runes) > 240 {
		message = string(runes[:240])
	}
	return message
}

func (s *browserSession) listen() {
	chromedp.ListenTarget(s.ctx, func(event any) {
		switch value := event.(type) {
		case *page.EventScreencastFrame:
			s.handleScreencastFrame(value)
		case *page.EventFrameNavigated, *page.EventNavigatedWithinDocument:
			if s.pageID() != "" {
				go s.clearInspectorOnNavigation()
				go s.refreshAndEmitState()
			}
		case *page.EventLoadEventFired:
			if s.pageID() != "" {
				go s.refreshAndEmitState()
			}
		}
	})
	go s.watchContext()
}

func (s *browserSession) watchContext() {
	<-s.ctx.Done()
	s.finish(false, "Chromium page exited")
}

func (m *browserManager) removeSession(session *browserSession) {
	m.mu.Lock()
	if m.sessions[session.tabID] == session {
		delete(m.sessions, session.tabID)
	}
	m.mu.Unlock()
}

func (s *browserSession) startScreencast() error {
	s.frameMu.Lock()
	if s.screencastStarted {
		s.frameMu.Unlock()
		s.clearFrameBackpressure()
		return nil
	}
	s.screencastStarted = true
	s.frameMu.Unlock()

	err := s.runCommand(func(ctx context.Context) error {
		return page.StartScreencast().
			WithFormat(page.ScreencastFormatJpeg).
			WithQuality(78).
			WithMaxWidth(4096).
			WithMaxHeight(4096).
			WithEveryNthFrame(1).
			Do(ctx)
	})
	if err != nil {
		s.frameMu.Lock()
		s.screencastStarted = false
		s.frameMu.Unlock()
	}
	return err
}

func (s *browserSession) handleScreencastFrame(frame *page.EventScreencastFrame) {
	pageID := s.pageID()
	if frame == nil || s.closed.Load() || pageID == "" {
		return
	}
	metadata := BrowserFrameMetadata{}
	if frame.Metadata != nil {
		metadata = BrowserFrameMetadata{
			OffsetTop:     frame.Metadata.OffsetTop,
			PageScale:     frame.Metadata.PageScaleFactor,
			DeviceWidth:   frame.Metadata.DeviceWidth,
			DeviceHeight:  frame.Metadata.DeviceHeight,
			ScrollOffsetX: frame.Metadata.ScrollOffsetX,
			ScrollOffsetY: frame.Metadata.ScrollOffsetY,
		}
	}

	s.frameEmitMu.Lock()
	defer s.frameEmitMu.Unlock()
	s.frameMu.Lock()
	staleCDPSessionID := s.pendingCDPSessionID
	s.frameSequence++
	sequence := s.frameSequence
	s.pendingFrameSequence = sequence
	s.pendingCDPSessionID = frame.SessionID
	s.frameMu.Unlock()
	if staleCDPSessionID != 0 && staleCDPSessionID != frame.SessionID {
		go s.ackCDPFrame(staleCDPSessionID)
	}
	s.manager.app.runtimeEvents.Emit(s.manager.app.bootContext(), browserFrameEvent, BrowserFramePayload{
		TabID:    s.tabID,
		PageID:   pageID,
		Sequence: sequence,
		Data:     frame.Data,
		Metadata: metadata,
	})
}

func (s *browserSession) captureAndEmitCurrentFrame(ctx context.Context) error {
	pageID := s.pageID()
	if s.closed.Load() || pageID == "" {
		return errors.New("browser session is closed")
	}
	image, err := page.CaptureScreenshot().
		WithFormat(page.CaptureScreenshotFormatJpeg).
		WithQuality(78).
		WithFromSurface(true).
		WithCaptureBeyondViewport(false).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("capture refreshed browser frame: %w", err)
	}
	state := s.view()
	s.frameEmitMu.Lock()
	defer s.frameEmitMu.Unlock()
	s.frameMu.Lock()
	s.frameSequence++
	sequence := s.frameSequence
	s.frameMu.Unlock()
	s.manager.app.runtimeEvents.Emit(s.manager.app.bootContext(), browserFrameEvent, BrowserFramePayload{
		TabID:    s.tabID,
		PageID:   pageID,
		Sequence: sequence,
		Data:     base64.StdEncoding.EncodeToString(image),
		Metadata: BrowserFrameMetadata{
			PageScale:    1,
			DeviceWidth:  float64(state.Width),
			DeviceHeight: float64(state.Height),
		},
	})
	return nil
}

func (s *browserSession) ackFrame(sequence uint64) error {
	s.frameMu.Lock()
	if sequence == 0 || sequence != s.pendingFrameSequence {
		s.frameMu.Unlock()
		return nil
	}
	cdpSessionID := s.pendingCDPSessionID
	s.pendingFrameSequence = 0
	s.pendingCDPSessionID = 0
	s.frameMu.Unlock()
	if cdpSessionID == 0 {
		return nil
	}
	return s.ackCDPFrame(cdpSessionID)
}

func (s *browserSession) ackCDPFrame(sessionID int64) error {
	return s.runCommand(func(ctx context.Context) error {
		return page.ScreencastFrameAck(sessionID).Do(ctx)
	})
}

func (s *browserSession) navigate(targetURL string) (BrowserSessionView, error) {
	err := s.runCommand(func(ctx context.Context) error {
		if err := chromedp.Navigate(targetURL).Do(ctx); err != nil {
			return err
		}
		return s.refreshStateLocked(ctx)
	})
	if err != nil {
		return BrowserSessionView{}, err
	}
	s.clearFrameBackpressure()
	return s.emitState(), nil
}

func (s *browserSession) reload() (BrowserSessionView, error) {
	err := s.runCommand(func(ctx context.Context) error {
		if err := page.Reload().Do(ctx); err != nil {
			return err
		}
		return s.refreshStateLocked(ctx)
	})
	if err != nil {
		return BrowserSessionView{}, err
	}
	s.clearFrameBackpressure()
	return s.emitState(), nil
}

func (s *browserSession) navigateHistory(delta int64) (BrowserSessionView, error) {
	err := s.runCommand(func(ctx context.Context) error {
		index, entries, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		targetIndex := index + delta
		if targetIndex < 0 || targetIndex >= int64(len(entries)) {
			return nil
		}
		if err := page.NavigateToHistoryEntry(entries[targetIndex].ID).Do(ctx); err != nil {
			return err
		}
		return s.refreshStateLocked(ctx)
	})
	if err != nil {
		return BrowserSessionView{}, err
	}
	s.clearFrameBackpressure()
	return s.emitState(), nil
}

func (s *browserSession) resize(width, height int) (BrowserSessionView, error) {
	width, height = clampBrowserViewport(width, height)
	if err := s.runCommand(func(ctx context.Context) error {
		return chromedp.EmulateViewport(int64(width), int64(height)).Do(ctx)
	}); err != nil {
		return BrowserSessionView{}, err
	}
	s.stateMu.Lock()
	s.state.Width = width
	s.state.Height = height
	s.state.Sequence++
	s.stateMu.Unlock()
	s.clearFrameBackpressure()
	return s.emitState(), nil
}

func (s *browserSession) mouse(event BrowserMouseEvent) error {
	typeValue, err := browserMouseType(event.Type)
	if err != nil {
		return err
	}
	button, err := browserMouseButton(event.Button)
	if err != nil {
		return err
	}
	x, y := s.clampPoint(event.X, event.Y)
	params := cdpinput.DispatchMouseEvent(typeValue, x, y).
		WithButton(button).
		WithButtons(event.Buttons).
		WithModifiers(browserModifiers(event.Modifiers)).
		WithDeltaX(event.DeltaX).
		WithDeltaY(event.DeltaY)
	if event.ClickCount > 0 {
		params = params.WithClickCount(event.ClickCount)
	}
	return s.runCommand(params.Do)
}

func (s *browserSession) key(event BrowserKeyEvent) error {
	typeValue, err := browserKeyType(event.Type)
	if err != nil {
		return err
	}
	params := cdpinput.DispatchKeyEvent(typeValue).
		WithKey(event.Key).
		WithCode(event.Code).
		WithText(event.Text).
		WithUnmodifiedText(event.UnmodifiedText).
		WithWindowsVirtualKeyCode(event.WindowsVirtualKeyCode).
		WithNativeVirtualKeyCode(event.NativeVirtualKeyCode).
		WithModifiers(browserModifiers(event.Modifiers)).
		WithAutoRepeat(event.AutoRepeat).
		WithIsKeypad(event.IsKeypad)
	return s.runCommand(params.Do)
}

func (s *browserSession) insertText(text string) error {
	if text == "" {
		return nil
	}
	if len(text) > 1<<20 {
		return errors.New("browser text input exceeds 1 MiB")
	}
	return s.runCommand(cdpinput.InsertText(text).Do)
}

func (s *browserSession) refreshAndEmitState() {
	if err := s.refreshState(); err == nil {
		s.emitState()
	}
}

func (s *browserSession) refreshState() error {
	return s.runCommand(s.refreshStateLocked)
}

func (s *browserSession) refreshStateLocked(ctx context.Context) error {
	var location, title string
	if err := chromedp.Location(&location).Do(ctx); err != nil {
		return err
	}
	if err := chromedp.Title(&title).Do(ctx); err != nil {
		return err
	}
	index, entries, err := page.GetNavigationHistory().Do(ctx)
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	if location != "" {
		s.state.URL = location
	}
	s.state.CanAnnotate = isLocalBrowserURL(s.state.URL)
	s.state.Title = title
	s.state.CanGoBack = index > 0
	s.state.CanGoForward = index+1 < int64(len(entries))
	s.state.Sequence++
	s.stateMu.Unlock()
	return nil
}

func (s *browserSession) runCommand(command func(context.Context) error) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.closed.Load() {
		return errors.New("browser page is closed")
	}
	ctx, cancel := context.WithTimeout(s.ctx, browserCommandTimeout)
	defer cancel()
	return chromedp.Run(ctx, chromedp.ActionFunc(command))
}

func (s *browserSession) emitState() BrowserSessionView {
	state := s.view()
	if state.PageID != "" {
		s.manager.app.runtimeEvents.Emit(s.manager.app.bootContext(), browserStateEvent, state)
	}
	return state
}

func (s *browserSession) clearFrameBackpressure() {
	s.frameMu.Lock()
	cdpSessionID := s.pendingCDPSessionID
	s.pendingFrameSequence = 0
	s.pendingCDPSessionID = 0
	s.frameMu.Unlock()
	if cdpSessionID != 0 {
		go s.ackCDPFrame(cdpSessionID)
	}
}

func (s *browserSession) clampPoint(x, y float64) (float64, float64) {
	state := s.view()
	if x < 0 {
		x = 0
	} else if x > float64(state.Width) {
		x = float64(state.Width)
	}
	if y < 0 {
		y = 0
	} else if y > float64(state.Height) {
		y = float64(state.Height)
	}
	return x, y
}

func (s *browserSession) view() BrowserSessionView {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

func (s *browserSession) pageID() string {
	return s.view().PageID
}

func (s *browserSession) setPageID(pageID string) {
	s.stateMu.Lock()
	s.state.PageID = pageID
	s.stateMu.Unlock()
}

func (s *browserSession) completeStart(err error) {
	s.readyOnce.Do(func() {
		s.startMu.Lock()
		s.startErr = err
		s.startMu.Unlock()
		close(s.ready)
	})
}

func (s *browserSession) waitReady() (BrowserSessionView, error) {
	<-s.ready
	s.startMu.Lock()
	err := s.startErr
	s.startMu.Unlock()
	if err != nil {
		return BrowserSessionView{}, err
	}
	if s.closed.Load() {
		return BrowserSessionView{}, errors.New("browser page is closed")
	}
	return s.view(), nil
}

func (s *browserSession) failStart(err error) {
	s.completeStart(err)
	s.finish(true, "")
}

func (s *browserSession) stop() {
	s.finish(true, "")
}

func (s *browserSession) finish(expected bool, message string) {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	if message == "" && !expected {
		message = "Chromium page exited"
	}
	s.completeStart(errors.New("browser page was closed"))
	s.stopScreencast()
	s.clearFrameBackpressure()
	s.cancel()
	s.manager.removeSession(s)
	if pageID := s.pageID(); pageID != "" {
		s.manager.app.runtimeEvents.Emit(s.manager.app.bootContext(), browserExitEvent, browserExitPayload{
			TabID:    s.tabID,
			PageID:   pageID,
			Error:    sanitizeBrowserError(message),
			Expected: expected,
		})
	}
}

func (s *browserSession) stopScreencast() {
	s.frameMu.Lock()
	started := s.screencastStarted
	s.screencastStarted = false
	s.frameMu.Unlock()
	if !started || s.ctx.Err() != nil {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	_ = chromedp.Run(ctx, page.StopScreencast())
}

func browserMouseType(value string) (cdpinput.MouseType, error) {
	switch value {
	case "mousePressed":
		return cdpinput.MousePressed, nil
	case "mouseReleased":
		return cdpinput.MouseReleased, nil
	case "mouseMoved":
		return cdpinput.MouseMoved, nil
	case "mouseWheel":
		return cdpinput.MouseWheel, nil
	default:
		return "", fmt.Errorf("unsupported browser mouse event %q", value)
	}
}

func browserMouseButton(value string) (cdpinput.MouseButton, error) {
	switch value {
	case "", "none":
		return cdpinput.None, nil
	case "left":
		return cdpinput.Left, nil
	case "middle":
		return cdpinput.Middle, nil
	case "right":
		return cdpinput.Right, nil
	case "back":
		return cdpinput.Back, nil
	case "forward":
		return cdpinput.Forward, nil
	default:
		return "", fmt.Errorf("unsupported browser mouse button %q", value)
	}
}

func browserKeyType(value string) (cdpinput.KeyType, error) {
	switch value {
	case "keyDown":
		return cdpinput.KeyDown, nil
	case "keyUp":
		return cdpinput.KeyUp, nil
	case "rawKeyDown":
		return cdpinput.KeyRawDown, nil
	case "char":
		return cdpinput.KeyChar, nil
	default:
		return "", fmt.Errorf("unsupported browser key event %q", value)
	}
}

func browserModifiers(value int64) cdpinput.Modifier {
	return cdpinput.Modifier(value & 15)
}

func browserProfilePath() (string, error) {
	root := strings.TrimSpace(config.ReasonixHomeDir())
	if root == "" {
		return "", errors.New("Reasonix home directory is unavailable")
	}
	return filepath.Join(root, "browser", "profile"), nil
}

func normalizeBrowserURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "about:blank" {
		return "about:blank", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse browser URL: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if strings.TrimSpace(parsed.Hostname()) == "" {
			return "", errors.New("browser URL requires a host")
		}
		return parsed.String(), nil
	case "file":
		return "", errors.New("file URLs are not allowed in the embedded browser")
	default:
		return "", fmt.Errorf("browser URL scheme %q is not allowed", parsed.Scheme)
	}
}

func isLocalBrowserURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func clampBrowserViewport(width, height int) (int, int) {
	if width < 320 {
		width = browserDefaultWidth
	}
	if height < 240 {
		height = browserDefaultHeight
	}
	if width > 4096 {
		width = 4096
	}
	if height > 4096 {
		height = 4096
	}
	return width, height
}

func resolveChromiumExecutable() (string, error) {
	var candidates []string
	if override := strings.TrimSpace(os.Getenv(browserExecutableEnv)); override != "" {
		candidates = append(candidates, override)
	}
	executable, err := os.Executable()
	if err == nil {
		candidates = append(candidates, bundledChromiumCandidates(filepath.Dir(executable), goruntime.GOOS, goruntime.GOARCH)...)
	}
	if version == "dev" {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			candidates = append(candidates, developmentChromiumCandidates(cwd, goruntime.GOOS, goruntime.GOARCH)...)
		}
	}
	for _, candidate := range candidates {
		path, pathErr := validExecutableFile(candidate)
		if pathErr == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("bundled Chromium is missing; set %s for development or reinstall Reasonix", browserExecutableEnv)
}

func bundledChromiumCandidates(base, goos, goarch string) []string {
	if strings.TrimSpace(base) == "" {
		return nil
	}
	switch goos {
	case "windows":
		return []string{filepath.Join(base, "chromium", "chrome.exe")}
	case "darwin":
		return []string{filepath.Join(base, "..", "Resources", "chromium", goarch, "Chromium.app", "Contents", "MacOS", "Google Chrome for Testing")}
	default:
		return []string{
			filepath.Join(base, "chromium", "chrome"),
			filepath.Join("/usr/lib/reasonix/chromium", "chrome"),
		}
	}
}

func developmentChromiumCandidates(cwd, goos, goarch string) []string {
	if strings.TrimSpace(cwd) == "" {
		return nil
	}
	platform := goos + "-" + goarch
	leaf := "chrome"
	if goos == "windows" {
		leaf = "chrome.exe"
	} else if goos == "darwin" {
		leaf = filepath.Join("Chromium.app", "Contents", "MacOS", "Google Chrome for Testing")
	}
	return []string{
		filepath.Join(cwd, "build", "runtime", "chromium", platform, leaf),
		filepath.Join(cwd, "desktop", "build", "runtime", "chromium", platform, leaf),
	}
}

func validExecutableFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("empty executable path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("Chromium executable path is a directory")
	}
	if goruntime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("Chromium executable is not marked executable")
	}
	return absolute, nil
}
