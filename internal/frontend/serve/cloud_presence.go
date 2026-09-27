package serve

import (
	"slices"
	"strings"
	"sync"
	"time"
)

type CloudControllerView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ConnectedAt time.Time `json:"connectedAt"`
	LastSeen    time.Time `json:"lastSeen"`
	Ordinal     int       `json:"ordinal"`
}

type cloudPresence struct {
	mu          sync.Mutex
	now         func() time.Time
	nextOrdinal int
	controllers map[string]CloudControllerView
}

func newCloudPresence() *cloudPresence {
	return &cloudPresence{now: time.Now, controllers: map[string]CloudControllerView{}}
}

func (p *cloudPresence) connect(id, name string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if current, ok := p.controllers[id]; ok {
		current.LastSeen = p.now()
		p.controllers[id] = current
		return current.Ordinal
	}
	p.nextOrdinal++
	now := p.now()
	p.controllers[id] = CloudControllerView{
		ID: id, Name: deviceLabel(name), ConnectedAt: now, LastSeen: now, Ordinal: p.nextOrdinal,
	}
	return p.nextOrdinal
}

func (p *cloudPresence) touch(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if current, ok := p.controllers[id]; ok {
		current.LastSeen = p.now()
		p.controllers[id] = current
	}
}

func (p *cloudPresence) disconnect(id string) {
	p.mu.Lock()
	delete(p.controllers, id)
	p.mu.Unlock()
}

func (p *cloudPresence) contains(id string) bool {
	p.mu.Lock()
	_, ok := p.controllers[id]
	p.mu.Unlock()
	return ok
}

func (p *cloudPresence) views() []CloudControllerView {
	p.mu.Lock()
	out := make([]CloudControllerView, 0, len(p.controllers))
	for _, controller := range p.controllers {
		out = append(out, controller)
	}
	p.mu.Unlock()
	slices.SortFunc(out, func(a, b CloudControllerView) int { return a.Ordinal - b.Ordinal })
	return out
}

func (h *Hub) CloudControllerConnected(id, name string) int {
	if h.opts.Share == nil {
		return 0
	}
	return h.opts.Share.cloud.connect(id, name)
}

func (h *Hub) CloudControllerSeen(id string) {
	if h.opts.Share != nil {
		h.opts.Share.cloud.touch(id)
	}
}

func (h *Hub) CloudControllerDisconnected(id string) {
	if h.opts.Share != nil {
		h.opts.Share.cloud.disconnect(id)
	}
}

// SetCloudControllerDisconnect gives the host-only sharing panel a way to
// close one encrypted Internet controller through the relay.
func (h *Hub) SetCloudControllerDisconnect(disconnect func(string) error) {
	if h.opts.Share != nil {
		h.opts.Share.setCloudDisconnect(disconnect)
	}
}

// SetCloudRemoteStatus lets the host-only device panel show a QR code for
// this Studio's own relay identity without exposing its device credential.
func (h *Hub) SetCloudRemoteStatus(status func() CloudRemoteStatus) {
	if h.opts.Share != nil {
		h.opts.Share.setCloudStatus(status)
	}
}
