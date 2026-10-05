package control

import (
	"context"
	"errors"
)

// ErrBrowserUnavailable means this controller has no browser session.
var ErrBrowserUnavailable = errors.New("this session has no browser")

// BrowserClose releases a page shared by the person and the agent.
func (c *Controller) BrowserClose(ctx context.Context, tabID string) error {
	if c == nil || c.browser == nil {
		return ErrBrowserUnavailable
	}
	return c.browser.CloseTab(ctx, tabID)
}
