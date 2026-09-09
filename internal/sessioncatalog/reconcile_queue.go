package sessioncatalog

import (
	"context"
	"strings"
	"time"
)

// RequestReconcile makes the channel a wake signal while the maps retain the
// newest target. Session saves never wait for catalog work.
func (c *Catalog) RequestReconcile(target DirectoryTarget) bool {
	if c == nil || strings.TrimSpace(target.Path) == "" {
		return false
	}
	target.Path = cleanCatalogAccessPath(target.Path)
	key := queuePathKey(target.Path)
	if key == "" {
		return false
	}
	target.mutationSeq = c.mutationSeq.Add(1)
	if _, loaded := c.reconcileQueued.LoadOrStore(key, target); loaded {
		c.markReconcileDirty(target)
		return true
	}
	select {
	case c.reconcileCh <- target:
		return true
	case <-c.stop:
		c.reconcileQueued.Delete(key)
		return false
	default:
		c.reconcileQueued.Delete(key)
		c.markReconcileDirty(target)
		return false
	}
}

func (c *Catalog) markReconcileDirty(target DirectoryTarget) {
	key := queuePathKey(target.Path)
	c.reconcileDirtyMu.Lock()
	if queued, ok := c.reconcileQueued.Load(key); ok {
		target = newestReconcileTarget(queued.(DirectoryTarget), target)
	}
	if dirty, ok := c.reconcileDirty[key]; ok {
		target = newestReconcileTarget(dirty, target)
	}
	c.reconcileDirty[key] = target
	c.reconcileQueued.Store(key, target)
	c.reconcileDirtyMu.Unlock()
}

func (c *Catalog) resolveReconcileToken(target DirectoryTarget) (DirectoryTarget, bool) {
	key := queuePathKey(target.Path)
	c.reconcileDirtyMu.Lock()
	defer c.reconcileDirtyMu.Unlock()
	queued, owned := c.reconcileQueued.Load(key)
	if !owned {
		return DirectoryTarget{}, false
	}
	target = newestReconcileTarget(target, queued.(DirectoryTarget))
	if latest, dirty := c.reconcileDirty[key]; dirty {
		target = newestReconcileTarget(target, latest)
		delete(c.reconcileDirty, key)
	}
	c.reconcileQueued.Store(key, target)
	return target, true
}

func newestReconcileTarget(current, candidate DirectoryTarget) DirectoryTarget {
	if candidate.mutationSeq > current.mutationSeq {
		return candidate
	}
	return current
}

func (c *Catalog) takeReconcileDirty() (DirectoryTarget, bool) {
	c.reconcileDirtyMu.Lock()
	defer c.reconcileDirtyMu.Unlock()
	for key, target := range c.reconcileDirty {
		delete(c.reconcileDirty, key)
		c.reconcileQueued.Store(key, target)
		return target, true
	}
	return DirectoryTarget{}, false
}

func (c *Catalog) reconcileLoop() {
	defer c.workers.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case token := <-c.reconcileCh:
			if target, ok := c.resolveReconcileToken(token); ok {
				c.runQueuedReconcile(target)
			}
			continue
		default:
		}
		if target, ok := c.takeReconcileDirty(); ok {
			c.runQueuedReconcile(target)
			continue
		}
		select {
		case token := <-c.reconcileCh:
			if target, ok := c.resolveReconcileToken(token); ok {
				c.runQueuedReconcile(target)
			}
		case <-ticker.C:
		case <-c.stop:
			return
		}
	}
}

func (c *Catalog) runQueuedReconcile(target DirectoryTarget) {
	key := queuePathKey(target.Path)
	for {
		if c.testReconcileStartHook != nil {
			c.testReconcileStartHook(target)
		}
		ctx, cancel := context.WithTimeout(c.workerCtx, 2*time.Minute)
		_ = c.reconcileDirectory(ctx, target, target.mutationSeq)
		cancel()

		c.reconcileDirtyMu.Lock()
		followUp, dirty := c.reconcileDirty[key]
		if dirty {
			delete(c.reconcileDirty, key)
			c.reconcileQueued.Store(key, followUp)
			c.reconcileDirtyMu.Unlock()
			target = followUp
			continue
		}
		c.reconcileQueued.Delete(key)
		c.reconcileDirtyMu.Unlock()
		return
	}
}
