package control

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"

	"reasonix/internal/attachment"
	"reasonix/internal/sessioncontent"
)

var hostVariantCache = attachment.NewVariantCache(attachment.DefaultCacheBytes, attachment.DefaultTransforms)

type controllerAttachmentState struct {
	attachments          atomic.Pointer[attachment.Service]
	attachmentOwnerMu    sync.Mutex
	attachmentOwnerScope string
	imageRoutesOnce      sync.Once
	imageRoutes          map[string]ImageRequestRoute
	imageRoutesErr       error
}

func (c *Controller) AttachmentOwnerIdentity() string {
	ref, _ := c.SessionRef()
	return ref.HostID + "\x00" + c.attachmentScope()
}

// NewAttachmentOperationContext binds an operation to both caller and owner.
func (c *Controller) NewAttachmentOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(c.attachmentContext(), cancel)
	return ctx, func() { stop(); cancel() }
}

func (c *Controller) attachmentContext() context.Context {
	if c.goalDriverControl.ctx != nil {
		return c.goalDriverControl.ctx
	}
	return context.Background()
}

func (c *Controller) attachmentService() *attachment.Service {
	if c == nil {
		return nil
	}
	return c.attachments.Load()
}

// Only the session publication owner changes this binding. Request goroutines
// retain their service snapshot; they never lazily choose another content root.
func (c *Controller) bindAttachmentService() {
	c.attachmentOwnerMu.Lock()
	defer c.attachmentOwnerMu.Unlock()
	content := c.sessionContentStore()
	scope := c.attachmentScope()
	current := c.attachments.Load()
	if current != nil && c.attachmentOwnerScope != scope {
		current.Drafts().ReleaseScope(c.attachmentOwnerScope)
	}
	c.attachmentOwnerScope = scope
	if current != nil && current.Store().Root() == content.Root() {
		return
	}
	c.attachments.Store(attachment.NewService(content, hostVariantCache))
}

func (c *Controller) sessionContentStore() *sessioncontent.Store {
	if c == nil {
		return nil
	}
	if _, runtime, _ := c.v3Binding(); runtime != nil {
		return runtime.Session().ContentStore()
	}
	if store := c.sessionEventStore(); store != nil {
		if content := store.ContentStore(); content != nil {
			return content
		}
	}
	if c.sessionDir != "" {
		return sessioncontent.New(filepath.Join(c.sessionDir, ".content-v1"))
	}
	if c.sessionPath != "" {
		return sessioncontent.New(filepath.Join(filepath.Dir(c.sessionPath), ".content-v1"))
	}
	if c.workspaceRoot != "" {
		return sessioncontent.New(filepath.Join(c.workspaceRoot, ".reasonix", "content-v1"))
	}
	return nil
}

func (c *Controller) attachmentScope() string {
	if c == nil {
		return ""
	}
	if _, runtime, _ := c.v3Binding(); runtime != nil {
		store := runtime.Session()
		return store.ID() + ":" + store.StorageGeneration()
	}
	if store := c.sessionEventStore(); store != nil {
		return store.ID() + ":" + store.StorageGeneration()
	}
	return c.sessionPath
}
