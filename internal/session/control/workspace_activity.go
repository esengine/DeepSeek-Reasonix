package control

import (
	"context"
	"sync"

	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/jobs"
)

type workspaceActivity struct {
	mu           sync.Mutex
	readers      int
	jobs         int
	checkout     chan struct{}
	inboxWaiters map[*Controller]struct{}
	references   int
}

var workspaceActivities = struct {
	sync.Mutex
	entries map[string]*workspaceActivity
}{entries: make(map[string]*workspaceActivity)}

// Entries live only while an operation owns or is waiting for its guard.
func (c *Controller) workspaceActivity() (*workspaceActivity, func(), error) {
	root := c.workspaceRepo.WorkTree
	if root == "" {
		root = c.workspaceRoot
	}
	if root == "" {
		return nil, func() {}, nil
	}
	key, err := workspacelease.CanonicalWorkspace(root)
	if err != nil {
		return nil, nil, err
	}
	workspaceActivities.Lock()
	activity := workspaceActivities.entries[key]
	if activity == nil {
		activity = &workspaceActivity{}
		workspaceActivities.entries[key] = activity
	}
	activity.references++
	workspaceActivities.Unlock()
	return activity, func() {
		workspaceActivities.Lock()
		defer workspaceActivities.Unlock()
		activity.references--
		if activity.references == 0 {
			delete(workspaceActivities.entries, key)
		}
	}, nil
}

type workspaceActivityUse struct {
	activity         *workspaceActivity
	releaseReference func()
	background       bool
}

func (u *workspaceActivityUse) release() {
	if u == nil {
		return
	}
	if a := u.activity; a != nil {
		a.mu.Lock()
		if u.background {
			a.jobs--
		} else {
			a.readers--
		}
		a.mu.Unlock()
	}
	u.releaseReference()
}

func (u *workspaceActivityUse) retainJobs() {
	if a := u.activity; a != nil {
		a.mu.Lock()
		a.readers--
		a.jobs++
		u.background = true
		a.mu.Unlock()
	}
}

func (c *Controller) holdWorkspaceActivity(ctx context.Context) (*workspaceActivityUse, error) {
	return c.acquireWorkspaceActivity(ctx, true)
}

func (c *Controller) tryWorkspaceActivity() (*workspaceActivityUse, error) {
	return c.acquireWorkspaceActivity(context.Background(), false)
}

func (c *Controller) acquireWorkspaceActivity(ctx context.Context, wait bool) (*workspaceActivityUse, error) {
	activity, release, err := c.workspaceActivity()
	if err != nil {
		return nil, err
	}
	use := &workspaceActivityUse{activity: activity, releaseReference: release}
	if activity == nil {
		return use, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		activity.mu.Lock()
		done := activity.checkout
		if done == nil {
			activity.readers++
			activity.mu.Unlock()
			return use, nil
		}
		activity.mu.Unlock()
		if !wait {
			release()
			return nil, ErrTurnRunning
		}
		select {
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		case <-done:
		}
	}
}

func (a *workspaceActivity) conflictLocked() error {
	if a.readers != 0 {
		return ErrTurnRunning
	}
	if a.jobs != 0 {
		return ErrJobsRunning
	}
	if a.checkout != nil {
		return ErrWorkspaceBusy
	}
	return nil
}

func (c *Controller) workspaceActivityConflict() error {
	activity, release, err := c.workspaceActivity()
	if err != nil {
		return err
	}
	defer release()
	if activity == nil {
		return nil
	}
	activity.mu.Lock()
	defer activity.mu.Unlock()
	return activity.conflictLocked()
}

func (c *Controller) excludeWorkspaceActivity() (func(), error) {
	activity, release, err := c.workspaceActivity()
	if err != nil {
		return nil, err
	}
	if activity == nil {
		return release, nil
	}
	activity.mu.Lock()
	if err := activity.conflictLocked(); err != nil {
		activity.mu.Unlock()
		release()
		return nil, err
	}
	activity.checkout = make(chan struct{})
	activity.mu.Unlock()
	return func() {
		activity.mu.Lock()
		close(activity.checkout)
		activity.checkout = nil
		waiters := activity.inboxWaiters
		activity.inboxWaiters = nil
		activity.mu.Unlock()
		release()
		for waiting := range waiters {
			go waiting.maybeDispatchInbox()
		}
	}, nil
}

// A retry reads the durable queue again; no user input body waits in memory.
func (c *Controller) resumeInboxAfterWorkspaceCheckout() {
	activity, release, err := c.workspaceActivity()
	if err != nil {
		c.notice("input remains queued: workspace unavailable: " + err.Error())
		return
	}
	defer release()
	if activity != nil {
		activity.mu.Lock()
		if activity.checkout != nil {
			if activity.inboxWaiters == nil {
				activity.inboxWaiters = make(map[*Controller]struct{})
			}
			activity.inboxWaiters[c] = struct{}{}
			activity.mu.Unlock()
			return
		}
		activity.mu.Unlock()
	}
	go c.maybeDispatchInbox()
}

// Cancellation does not end a background process; its done channel does.
func (c *Controller) releaseWorkspaceTurn(use *workspaceActivityUse) {
	if use == nil {
		return
	}
	if c.jobs == nil || !c.jobs.HasUnfinishedForSession("") {
		use.release()
		return
	}
	use.retainJobs()
	go func() {
		defer use.release()
		for {
			running := c.jobs.Running()
			if len(running) == 0 {
				return
			}
			ids := make([]string, len(running))
			for i, j := range running {
				ids[i] = j.ID
			}
			c.jobs.Wait(context.Background(), ids, jobs.WaitOptions{})
		}
	}()
}
