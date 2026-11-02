package sessionstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"reasonix/internal/base/backgroundwork"
	"reasonix/internal/state/sessionv4"
)

type ListingStatus struct {
	ListingState    string `json:"listingState,omitempty"`
	ListingRevision uint64 `json:"listingRevision,omitempty"`
	CountsComplete  bool   `json:"countsComplete"`
	PendingMetadata int    `json:"pendingMetadata,omitempty"`
	FailedMetadata  int    `json:"failedMetadata,omitempty"`
}

type legacyRow struct {
	source  sessionv4.Session
	listing sessionv4.Listing
	state   string
	retry   time.Time
}

type legacyDirectory struct {
	rows         map[string]*legacyRow
	status       ListingStatus
	scanAt       time.Time
	scanNeeded   bool
	priority     map[string]bool
	writeAt      time.Time
	writes       int
	cacheErr     error
	scanFailures int
}

type LegacyListings struct {
	mu         sync.Mutex
	dirs       map[string]*legacyDirectory
	observers  map[*func()]struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	wake       chan struct{}
	replay     legacyReplay
	changed    bool
	lastNotice time.Time
}

var sharedLegacyListings struct {
	sync.Mutex
	service *LegacyListings
	refs    int
}

func AcquireLegacyListings() (*LegacyListings, func()) {
	sharedLegacyListings.Lock()
	defer sharedLegacyListings.Unlock()
	if sharedLegacyListings.service == nil {
		sharedLegacyListings.service = newLegacyListings(readLegacyTranscript)
	}
	s := sharedLegacyListings.service
	sharedLegacyListings.refs++
	var once sync.Once
	return s, func() {
		once.Do(func() {
			sharedLegacyListings.Lock()
			sharedLegacyListings.refs--
			if sharedLegacyListings.refs == 0 {
				sharedLegacyListings.service = nil
			}
			last := sharedLegacyListings.refs == 0
			sharedLegacyListings.Unlock()
			if last {
				s.Close()
			}
		})
	}
}

func newLegacyListings(replay legacyReplay) *LegacyListings {
	ctx, cancel := context.WithCancel(context.Background())
	s := &LegacyListings{dirs: map[string]*legacyDirectory{}, observers: map[*func()]struct{}{}, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), replay: replay}
	go s.run()
	return s
}

func (s *LegacyListings) Close() { s.cancel(); <-s.done }

func (s *LegacyListings) Subscribe(fn func()) func() {
	s.mu.Lock()
	s.observers[&fn] = struct{}{}
	s.mu.Unlock()
	return func() { s.mu.Lock(); delete(s.observers, &fn); s.mu.Unlock() }
}

func (s *LegacyListings) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *LegacyListings) Snapshot(dir string) ([]SessionInfo, ListingStatus) {
	dir = filepath.Clean(dir)
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.dirs[dir]
	if d == nil {
		d = &legacyDirectory{rows: map[string]*legacyRow{}, status: ListingStatus{ListingState: "discovering"}, scanNeeded: true, priority: map[string]bool{}}
		s.dirs[dir] = d
	} else if time.Now().After(d.scanAt) {
		d.scanNeeded = true
	}
	s.signal()
	rows := make([]SessionInfo, 0, len(d.rows))
	for _, row := range d.rows {
		if row.state == "ready" && row.listing.Turns == 0 {
			continue
		}
		info := legacySessionInfo(dir, row.source, row.listing, row.state == "ready")
		if !IsVisibleSession(info.Path) {
			continue
		}
		info.MetadataState = row.state
		rows = append(rows, info)
	}
	return rows, d.status
}

func (s *LegacyListings) Prioritize(dir string, paths []string) {
	s.mu.Lock()
	if d := s.dirs[filepath.Clean(dir)]; d != nil {
		d.priority = make(map[string]bool, len(paths))
		for _, path := range paths {
			d.priority[path] = true
		}
	}
	s.mu.Unlock()
	s.signal()
}

func (s *LegacyListings) Retry(dir string) {
	s.mu.Lock()
	if d := s.dirs[filepath.Clean(dir)]; d != nil {
		d.scanNeeded = true
		for _, row := range d.rows {
			if row.state == "error" {
				row.state, row.retry = "pending", time.Time{}
			}
		}
		s.updateStatus(d)
	}
	s.mu.Unlock()
	s.signal()
}

func (s *LegacyListings) run() {
	defer close(s.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for s.ctx.Err() == nil {
		s.notify()
		if s.step() {
			continue
		}
		select {
		case <-s.ctx.Done():
		case <-s.wake:
		case <-ticker.C:
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.mu.Lock()
	dirs := make([]string, 0, len(s.dirs))
	for dir := range s.dirs {
		dirs = append(dirs, dir)
	}
	s.mu.Unlock()
	for _, dir := range dirs {
		_ = resolverFor(dir).flush(ctx, dir, false)
	}
}

func (s *LegacyListings) notify() {
	s.mu.Lock()
	if !s.changed || time.Since(s.lastNotice) < 250*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.changed, s.lastNotice = false, time.Now()
	callbacks := make([]func(), 0, len(s.observers))
	for fn := range s.observers {
		callbacks = append(callbacks, *fn)
	}
	s.mu.Unlock()
	for _, fn := range callbacks {
		fn()
	}
}

func (s *LegacyListings) step() bool {
	s.mu.Lock()
	for dir, d := range s.dirs {
		if d.scanNeeded {
			d.scanNeeded = false
			d.scanAt = time.Now().Add(5 * time.Second)
			s.mu.Unlock()
			_ = backgroundwork.Shared.Do(s.ctx, backgroundwork.Visible, func(ctx context.Context) error { return s.scan(ctx, dir) })
			return true
		}
	}
	for dir, d := range s.dirs {
		if time.Now().Before(d.writeAt) && d.writes < 32 {
			continue
		}
		d.writeAt, d.writes = time.Now().Add(time.Second), 0
		s.mu.Unlock()
		err := resolverFor(dir).flush(s.ctx, dir, false)
		s.mu.Lock()
		wasDegraded := d.cacheErr != nil
		d.cacheErr = err
		if wasDegraded != (err != nil) {
			s.updateStatus(d)
		}
		s.mu.Unlock()
		return true
	}
	var chosen *legacyRow
	var chosenDir string
	var preferred bool
	for dir, d := range s.dirs {
		for _, row := range d.rows {
			if row.state == "ready" || time.Now().Before(row.retry) {
				continue
			}
			priority := d.priority[v4ImportPath(dir, row.source.Manifest.SessionID)]
			if chosen == nil || priority && !preferred || priority == preferred && row.source.UpdatedAt().After(chosen.source.UpdatedAt()) {
				chosen, chosenDir, preferred = row, dir, priority
			}
		}
	}
	if chosen != nil {
		s.mu.Unlock()
		var listing sessionv4.Listing
		priority := backgroundwork.Rebuild
		if preferred {
			priority = backgroundwork.Visible
		}
		err := backgroundwork.Shared.Do(s.ctx, priority, func(ctx context.Context) error {
			var err error
			listing, err = resolverFor(chosenDir).resolve(ctx, chosenDir, chosen.source, s.replay)
			return err
		})
		s.mu.Lock()
		d := s.dirs[chosenDir]
		if err == nil {
			chosen.listing, chosen.state = listing, "ready"
			d.writes++
		} else {
			chosen.state, chosen.retry = "error", time.Now().Add(time.Minute)
			if errors.Is(err, errLegacySourceChanged) {
				d.scanNeeded = true
			}
		}
		s.updateStatus(d)
		s.mu.Unlock()
		return true
	}
	s.mu.Unlock()
	return false
}

func (s *LegacyListings) updateStatus(d *legacyDirectory) {
	d.status.PendingMetadata, d.status.FailedMetadata = 0, d.scanFailures
	for _, row := range d.rows {
		if row.state == "pending" {
			d.status.PendingMetadata++
		}
		if row.state == "error" {
			d.status.FailedMetadata++
		}
	}
	if d.status.ListingState != "discovering" {
		d.status.CountsComplete = d.status.PendingMetadata == 0 && d.status.FailedMetadata == 0
		d.status.ListingState = "ready"
		if d.status.PendingMetadata > 0 {
			d.status.ListingState = "indexing"
		}
		if d.status.FailedMetadata > 0 || d.cacheErr != nil {
			d.status.ListingState = "degraded"
		}
	}
	d.status.ListingRevision++
	s.changed = true
}

func (s *LegacyListings) scan(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(v4RootFor(dir))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	seen := map[string]bool{}
	failures := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id := entry.Name()
		if !entry.IsDir() || !isV4SessionID(id) {
			continue
		}
		path := v4ImportPath(dir, id)
		if !IsVisibleSession(path) || sessionArtifactExists(path) {
			continue
		}
		source, openErr := sessionv4.Open(filepath.Join(v4RootFor(dir), id))
		if openErr != nil {
			seen[id] = true
			failures++
			continue
		}
		seen[id] = true
		r := resolverFor(dir)
		release, lockErr := r.acquire(ctx, dir)
		if lockErr != nil {
			return lockErr
		}
		listing, ok := r.lookup(source)
		release()
		s.mu.Lock()
		d := s.dirs[dir]
		previous := d.rows[id]
		if previous == nil || !previous.source.Manifest.Equal(source.Manifest) || previous.source.LogSize != source.LogSize || !previous.source.LogModTime.Equal(source.LogModTime) || ok && previous.state != "ready" {
			state := "pending"
			if ok {
				state = "ready"
			}
			d.rows[id] = &legacyRow{source: source, listing: listing, state: state}
			s.updateStatus(d)
		}
		s.mu.Unlock()
		s.notify()
	}
	s.mu.Lock()
	d := s.dirs[dir]
	d.scanFailures = failures
	if err == nil {
		for id := range d.rows {
			if !seen[id] {
				delete(d.rows, id)
			}
		}
	}
	d.scanAt = time.Now().Add(5 * time.Second)
	d.status.ListingState = "indexing"
	s.updateStatus(d)
	if err != nil {
		d.status.ListingState, d.status.CountsComplete = "degraded", false
	}
	s.mu.Unlock()
	return nil
}

func legacySessionInfo(dir string, s sessionv4.Session, l sessionv4.Listing, known bool) SessionInfo {
	return SessionInfo{Path: v4ImportPath(dir, s.Manifest.SessionID), CreatedAt: s.Manifest.CreatedAt, LastActivityAt: s.UpdatedAt(), ModTime: s.UpdatedAt(), Preview: l.Preview, Turns: l.Turns, CountsKnown: known, CustomTitle: l.Title}
}

func sortLegacyRows(rows []SessionInfo) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].LastActivityAt.Equal(rows[j].LastActivityAt) {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].LastActivityAt.After(rows[j].LastActivityAt)
	})
}
