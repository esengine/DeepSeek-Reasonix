package configbackup

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"reasonix/internal/contract/config"
)

// Item status against this machine.
const (
	StatusNew     = "new"
	StatusChanged = "changed"
	StatusSame    = "same"
	// StatusInstall is a plugin: it comes back through the plugin install flow.
	StatusInstall = "install"
)

// Why an item needs its own consent before it is written.
const (
	ConsentExecutes = "executes"
	// ConsentEndpoint is a provider, or a default model, that would send
	// conversations and a stored key to an endpoint this machine does not use.
	ConsentEndpoint = "endpoint"
	// ConsentReplacesSecret overwrites a key already stored here.
	ConsentReplacesSecret = "replaces_secret"
	// ConsentImports is a standing-instruction file that imports other files.
	ConsentImports = "imports"
)

// PlanItem is one row of the restore preview.
type PlanItem struct {
	ID            string    `json:"id"`
	Category      Category  `json:"category"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	Consent       string    `json:"consent,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	Previous      string    `json:"previous,omitempty"`
	Paths         []PathRef `json:"paths,omitempty"`
	CrossPlatform bool      `json:"crossPlatform,omitempty"`
	Files         int       `json:"files,omitempty"`
	Details       []string  `json:"details,omitempty"`
	Content       string    `json:"content,omitempty"`
	// Recommended is the default tick: a change that is safe to take as-is.
	Recommended bool `json:"recommended"`
}

// Plan is the preview of one decrypted backup.
type Plan struct {
	ID           string     `json:"planId"`
	CreatedAt    time.Time  `json:"createdAt"`
	AppVersion   string     `json:"appVersion,omitempty"`
	Platform     string     `json:"platform"`
	SamePlatform bool       `json:"samePlatform"`
	Categories   []Category `json:"categories"`
	Items        []PlanItem `json:"items"`
	Omitted      []Omission `json:"omitted,omitempty"`
}

// ErrPlanExpired is an apply whose plan this process no longer holds.
var ErrPlanExpired = errors.New("configbackup: restore preview expired; open the backup again")

const (
	planTTL     = 15 * time.Minute
	maxHeldPlan = 4
)

// Planner holds decrypted snapshots between preview and apply, so the apply
// acts on exactly what was previewed and the passphrase is asked for once.
type Planner struct {
	mu    sync.Mutex
	plans map[string]heldPlan
	now   func() time.Time
}

type heldPlan struct {
	snapshot *Snapshot
	expires  time.Time
}

// NewPlanner returns an empty planner.
func NewPlanner() *Planner { return &Planner{plans: map[string]heldPlan{}, now: time.Now} }

// Preview compares a snapshot with this machine and holds it for Apply.
func (p *Planner) Preview(s *Snapshot) (*Plan, error) {
	local, err := collectLocal(s)
	if err != nil {
		return nil, err
	}
	id, err := newPlanID()
	if err != nil {
		return nil, err
	}
	plan := &Plan{
		ID: id, CreatedAt: s.CreatedAt, AppVersion: s.AppVersion, Platform: s.Platform,
		SamePlatform: samePlatform(s.Platform), Categories: s.Categories, Omitted: s.Omitted,
	}
	for _, it := range s.Items {
		plan.Items = append(plan.Items, planItem(it, s, local, plan.SamePlatform))
	}
	p.hold(id, s)
	return plan, nil
}

func (p *Planner) hold(id string, s *Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for k, h := range p.plans {
		if now.After(h.expires) {
			delete(p.plans, k)
		}
	}
	for len(p.plans) >= maxHeldPlan {
		oldest := ""
		for k, h := range p.plans {
			if oldest == "" || h.expires.Before(p.plans[oldest].expires) {
				oldest = k
			}
		}
		delete(p.plans, oldest)
	}
	p.plans[id] = heldPlan{snapshot: s, expires: now.Add(planTTL)}
}

// take removes a plan: one preview backs one apply, so a replayed request
// cannot write the same items twice.
func (p *Planner) take(id string) (*Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.plans[id]
	delete(p.plans, id)
	if !ok || p.now().After(h.expires) {
		return nil, ErrPlanExpired
	}
	return h.snapshot, nil
}

func newPlanID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func samePlatform(platform string) bool {
	goos, _, _ := strings.Cut(platform, "/")
	return goos == runtime.GOOS
}

// localState is this machine's view for the same items, keyed by item ID.
type localState struct {
	items     map[string]Item
	providers map[string]config.ProviderEntry
}

func collectLocal(s *Snapshot) (*localState, error) {
	c := newCollector(slices.Contains(s.Categories, CategorySecrets))
	if err := c.load(); err != nil {
		return nil, err
	}
	for _, cat := range Categories() {
		if err := c.collect(cat.ID); err != nil {
			return nil, err
		}
	}
	st := &localState{items: map[string]Item{}, providers: map[string]config.ProviderEntry{}}
	for _, it := range c.items {
		st.items[it.ID] = it
	}
	for _, p := range c.cfg.Providers {
		st.providers[p.Name] = p
	}
	return st, nil
}

func planItem(it Item, s *Snapshot, local *localState, same bool) PlanItem {
	row := PlanItem{ID: it.ID, Category: it.Category, Kind: it.Kind, Name: it.Name}
	row.Summary, row.Paths = describe(it)
	row.Details, row.Content = details(it)
	if it.Kind == KindSkill {
		var sk skillData
		if json.Unmarshal(it.Data, &sk) == nil {
			row.Files = len(sk.Files)
		}
	}
	have, exists := local.items[it.ID]
	switch {
	case it.Kind == KindPlugin:
		row.Status = StatusInstall
		if exists {
			row.Status = StatusSame
		}
	case !exists:
		row.Status = StatusNew
	case bytes.Equal(canonical(have.Data), canonical(it.Data)):
		row.Status = StatusSame
	default:
		row.Status = StatusChanged
		row.Previous, _ = describe(have)
	}
	row.Consent = consentFor(it, s, local)
	row.CrossPlatform = !same && executes(it.Kind)
	missing := slices.ContainsFunc(row.Paths, func(p PathRef) bool { return !p.Exists })
	row.Recommended = row.Status != StatusSame && row.Consent == "" && !missing && !row.CrossPlatform
	return row
}

func canonical(raw json.RawMessage) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	out, _ := json.Marshal(v)
	return out
}

func executes(kind string) bool {
	return kind == KindHook || kind == KindStatusline || kind == KindMCP
}
