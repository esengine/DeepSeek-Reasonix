// Package browser provides a browser automation service backed by rod,
// exposing navigation, interaction, screenshot, and JS evaluation primitives
// that are bound to the Wails frontend (App.Browser* methods). It launches a
// headless Chromium instance managed by rod.
package browser

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// ErrNotConnected is returned when no browser is available.
var ErrNotConnected = errors.New("browser not connected")

// Service provides browser automation primitives to the desktop frontend.
// It is safe for concurrent use.
type Service struct {
	mu      sync.Mutex
	browser *rod.Browser
	page    *rod.Page
	laun    *launcher.Launcher
	started bool
}

// New creates an unstarted browser service.
func New() *Service {
	return &Service{}
}

// Start launches a headless Chromium instance and opens a blank page.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	// Launch a headless Chromium (auto-downloads if needed).
	l := launcher.New().Headless(true)
	url, err := l.Launch()
	if err != nil {
		return fmt.Errorf("browser: launch failed: %w", err)
	}

	browser := rod.New().ControlURL(url).Trace(false)
	if err := browser.Connect(); err != nil {
		return fmt.Errorf("browser: connect failed: %w", err)
	}

	// Create a default blank page.
	page, err := browser.Page(proto.TargetCreateTarget{
		URL: "about:blank",
	})
	if err != nil {
		browser.Close()
		return fmt.Errorf("browser: create page failed: %w", err)
	}

	_ = page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             1280,
		Height:            800,
		DeviceScaleFactor: 1,
		Mobile:            false,
	})

	s.browser = browser
	s.page = page
	s.laun = l
	s.started = true

	log.Println("[browser] service started")
	return nil
}

// Stop closes the browser and releases resources.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	if s.page != nil {
		_ = s.page.Close()
		s.page = nil
	}
	if s.browser != nil {
		s.browser.Close()
		s.browser = nil
	}
	if s.laun != nil {
		s.laun.Kill()
		s.laun = nil
	}
	s.started = false
	log.Println("[browser] service stopped")
}

// ensureReady checks that the service is started and the page is alive.
func (s *Service) ensureReady() error {
	if !s.started || s.page == nil {
		return ErrNotConnected
	}
	return nil
}

// pageURLAndTitle returns the current page URL and title via CDP Info.
func (s *Service) pageURLAndTitle() (string, string) {
	info, err := s.page.Info()
	if err != nil {
		return "", ""
	}
	return info.URL, info.Title
}

// Navigate opens the given URL in the browser and waits for the page to load.
// Returns "currentURL|||pageTitle".
func (s *Service) Navigate(url string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return "", err
	}

	if err := s.page.Navigate(url); err != nil {
		return "", fmt.Errorf("browser: navigate failed: %w", err)
	}
	_ = s.page.WaitLoad()

	curURL, title := s.pageURLAndTitle()
	return fmt.Sprintf("%s|||%s", curURL, title), nil
}

// Back navigates one step back in history. Returns "currentURL|||pageTitle".
func (s *Service) Back() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return "", err
	}

	if err := s.page.NavigateBack(); err != nil {
		return "", fmt.Errorf("browser: back failed: %w", err)
	}
	_ = s.page.WaitLoad()

	curURL, title := s.pageURLAndTitle()
	return fmt.Sprintf("%s|||%s", curURL, title), nil
}

// Forward navigates one step forward in history. Returns "currentURL|||pageTitle".
func (s *Service) Forward() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return "", err
	}

	if err := s.page.NavigateForward(); err != nil {
		return "", fmt.Errorf("browser: forward failed: %w", err)
	}
	_ = s.page.WaitLoad()

	curURL, title := s.pageURLAndTitle()
	return fmt.Sprintf("%s|||%s", curURL, title), nil
}

// Refresh reloads the current page. Returns "currentURL|||pageTitle".
func (s *Service) Refresh() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return "", err
	}

	_ = s.page.Reload()
	_ = s.page.WaitLoad()

	curURL, title := s.pageURLAndTitle()
	return fmt.Sprintf("%s|||%s", curURL, title), nil
}

// Screenshot takes a screenshot of the current page and returns it as a
// data-URL (base64-encoded PNG). Set quality to nil for PNG, or 1-100 for JPEG.
func (s *Service) Screenshot() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return "", err
	}

	buf, err := s.page.Screenshot(false, nil)
	if err != nil {
		return "", fmt.Errorf("browser: screenshot failed: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(buf)
	return "data:image/png;base64," + encoded, nil
}

// Eval executes JavaScript in the current page and returns the result as a
// string. The JS expression must return a JSON-serializable value or void.
func (s *Service) Eval(js string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return "", err
	}

	result, err := s.page.Eval(js)
	if err != nil {
		return "", fmt.Errorf("browser: eval failed: %w", err)
	}

	if result == nil || result.Value.Nil() {
		return "", nil
	}

	return result.Value.String(), nil
}

// Click clicks on an element matching the given CSS selector.
func (s *Service) Click(selector string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return err
	}

	el, err := s.page.Element(selector)
	if err != nil {
		return fmt.Errorf("browser: element not found %q: %w", selector, err)
	}

	return el.Click("left", 1)
}

// ClickAtPoint clicks at specific coordinates (x, y) relative to the viewport.
func (s *Service) ClickAtPoint(x, y float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return err
	}

	// Move mouse to coordinates first, then click.
	if err := s.page.Mouse.MoveTo(proto.NewPoint(x, y)); err != nil {
		return fmt.Errorf("browser: move mouse failed: %w", err)
	}
	return s.page.Mouse.Click(proto.InputMouseButtonLeft, 1)
}

// Type types text into an element matching the given CSS selector.
func (s *Service) Type(selector, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return err
	}

	el, err := s.page.Element(selector)
	if err != nil {
		return fmt.Errorf("browser: element not found %q: %w", selector, err)
	}

	return el.Input(text)
}

// ScrollDown scrolls the page down by the given number of pixels.
func (s *Service) ScrollDown(pixels int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return err
	}

	return s.page.Mouse.Scroll(0, float64(pixels), 0)
}

// GetCurrentURL returns the current page URL and title.
func (s *Service) GetCurrentURL() (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return "", "", err
	}

	curURL, title := s.pageURLAndTitle()
	return curURL, title, nil
}

// WaitLoad waits for the page to finish loading, with a timeout.
func (s *Service) WaitLoad(timeout time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReady(); err != nil {
		return err
	}

	_ = s.page.WaitLoad()
	return nil
}

// IsRunning returns whether the browser service is currently running.
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}
