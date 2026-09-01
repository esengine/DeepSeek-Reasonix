package main

import (
	"fmt"
	"strings"
	"sync"
)

type navigationIntentFence struct {
	mu    sync.Mutex
	token string
	// beforeCloseFinalHook is test-only. It pauses after the off-lock snapshot
	// and before the navigation-fenced removal phase.
	beforeCloseFinalHook func()
}

// RegisterNavigationIntent publishes the latest frontend navigation intent.
// CloseMergedWorktreeTab checks the exact token at its removal linearization
// point, so an older async merge lifecycle cannot close a newly selected tab.
func (a *App) RegisterNavigationIntent(token string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 160 {
		return fmt.Errorf("navigation intent token is invalid")
	}
	for _, char := range token {
		if char < 0x21 || char > 0x7e {
			return fmt.Errorf("navigation intent token is invalid")
		}
	}
	a.navigationIntent.mu.Lock()
	a.navigationIntent.token = token
	a.navigationIntent.mu.Unlock()
	return nil
}

func (a *App) requireNavigationIntent(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("navigation intent token is required; resources were preserved")
	}
	a.navigationIntent.mu.Lock()
	current := a.navigationIntent.token
	a.navigationIntent.mu.Unlock()
	if current != token {
		return fmt.Errorf("navigation changed before worktree close; resources were preserved")
	}
	return nil
}
