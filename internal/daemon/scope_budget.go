package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/fileutil"
)

const scopeBudgetConfigVersion = 1

// ScopeBudgetConfig stores project/global aggregate budget limits for the
// daemon. Usage counters stay in per-session runtime sidecars; this file only
// stores quota policy.
type ScopeBudgetConfig struct {
	Version   int                `json:"version"`
	UpdatedAt time.Time          `json:"updated_at"`
	Quotas    []ScopeBudgetQuota `json:"quotas,omitempty"`
}

// ScopeBudgetQuota is an aggregate budget policy. A global quota applies across
// all daemon sessions; a project quota applies to sessions in one workspace.
type ScopeBudgetQuota struct {
	Scope               string  `json:"scope"` // global|project
	WorkspaceRoot       string  `json:"workspace_root,omitempty"`
	DailyModelCallLimit int     `json:"daily_model_call_limit,omitempty"`
	DailyModelCostLimit float64 `json:"daily_model_cost_limit,omitempty"`
}

// BudgetAggregatesResponse is the JSON body of GET /budgets.
type BudgetAggregatesResponse struct {
	Budgets []BudgetAggregateView `json:"budgets"`
}

// BudgetAggregateView is a project/global aggregate budget view.
type BudgetAggregateView struct {
	Scope               string     `json:"scope"`
	WorkspaceRoot       string     `json:"workspace_root,omitempty"`
	SessionCount        int        `json:"session_count"`
	DailyModelCallLimit int        `json:"daily_model_call_limit,omitempty"`
	DailyModelCalls     int        `json:"daily_model_calls,omitempty"`
	DailyModelCostLimit float64    `json:"daily_model_cost_limit,omitempty"`
	DailyModelCost      float64    `json:"daily_model_cost,omitempty"`
	ModelCostCurrency   string     `json:"model_cost_currency,omitempty"`
	WindowStartedAt     *time.Time `json:"window_started_at,omitempty"`
	Blocked             bool       `json:"blocked,omitempty"`
	LastBlockedReason   string     `json:"last_blocked_reason,omitempty"`
}

func (d *Daemon) scopeBudgetConfigPath() string {
	return filepath.Join(d.sessionDir, ".daemon.budgets.json")
}

func (d *Daemon) loadScopeBudgetConfig() (ScopeBudgetConfig, error) {
	path := d.scopeBudgetConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ScopeBudgetConfig{Version: scopeBudgetConfigVersion}, nil
		}
		return ScopeBudgetConfig{}, err
	}
	var cfg ScopeBudgetConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return ScopeBudgetConfig{}, fmt.Errorf("decode scope budget config: %w", err)
	}
	return cfg, nil
}

func (d *Daemon) saveScopeBudgetConfig(cfg ScopeBudgetConfig) error {
	cfg.Version = scopeBudgetConfigVersion
	cfg.UpdatedAt = time.Now().UTC()
	path := d.scopeBudgetConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daemon.budgets.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

func normalizeScopeBudgetQuota(q ScopeBudgetQuota) ScopeBudgetQuota {
	q.Scope = strings.TrimSpace(q.Scope)
	q.WorkspaceRoot = strings.TrimSpace(q.WorkspaceRoot)
	if q.Scope == "project" {
		q.WorkspaceRoot = cleanWorkspaceRoot(q.WorkspaceRoot)
	} else {
		q.WorkspaceRoot = ""
	}
	return q
}

func cleanWorkspaceRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Clean(root)
}

func upsertScopeBudgetQuota(cfg *ScopeBudgetConfig, quota ScopeBudgetQuota) {
	quota = normalizeScopeBudgetQuota(quota)
	for i := range cfg.Quotas {
		existing := normalizeScopeBudgetQuota(cfg.Quotas[i])
		if existing.Scope == quota.Scope && sameWorkspaceRoot(existing.WorkspaceRoot, quota.WorkspaceRoot) {
			cfg.Quotas[i] = quota
			return
		}
	}
	cfg.Quotas = append(cfg.Quotas, quota)
	sort.Slice(cfg.Quotas, func(i, j int) bool {
		if cfg.Quotas[i].Scope != cfg.Quotas[j].Scope {
			return cfg.Quotas[i].Scope < cfg.Quotas[j].Scope
		}
		return cfg.Quotas[i].WorkspaceRoot < cfg.Quotas[j].WorkspaceRoot
	})
}

func (d *Daemon) checkScopeModelBudgetLocked(entry *SessionEntry, source string, now time.Time) (bool, string) {
	cfg, err := d.loadScopeBudgetConfig()
	if err != nil {
		d.logger.Warn("daemon: load scope budget config", "err", err)
		return true, ""
	}
	for _, quota := range matchingScopeBudgetQuotas(entry, cfg.Quotas) {
		view := d.aggregateBudgetLocked(quota.Scope, quota.WorkspaceRoot, []ScopeBudgetQuota{quota}, now)
		if quota.DailyModelCallLimit > 0 && view.DailyModelCalls >= quota.DailyModelCallLimit {
			reason := fmt.Sprintf("%s daily model call budget exhausted for %s (%d/%d)", scopeBudgetLabel(quota), source, view.DailyModelCalls, quota.DailyModelCallLimit)
			entry.Runtime.Budget.LastBlockedAt = now.UTC()
			entry.Runtime.Budget.LastBlockedReason = reason
			return false, reason
		}
		if quota.DailyModelCostLimit > 0 && view.DailyModelCost >= quota.DailyModelCostLimit {
			reason := fmt.Sprintf("%s daily model cost budget exhausted for %s (%.6f/%.6f)", scopeBudgetLabel(quota), source, view.DailyModelCost, quota.DailyModelCostLimit)
			entry.Runtime.Budget.LastBlockedAt = now.UTC()
			entry.Runtime.Budget.LastBlockedReason = reason
			return false, reason
		}
	}
	return true, ""
}

func matchingScopeBudgetQuotas(entry *SessionEntry, quotas []ScopeBudgetQuota) []ScopeBudgetQuota {
	if entry == nil {
		return nil
	}
	entryScope, entryRoot := entryScopeAndRoot(entry)
	var out []ScopeBudgetQuota
	for _, quota := range quotas {
		quota = normalizeScopeBudgetQuota(quota)
		switch quota.Scope {
		case "global":
			out = append(out, quota)
		case "project":
			if entryScope == "project" && sameWorkspaceRoot(entryRoot, quota.WorkspaceRoot) {
				out = append(out, quota)
			}
		}
	}
	return out
}

func scopeBudgetLabel(quota ScopeBudgetQuota) string {
	quota = normalizeScopeBudgetQuota(quota)
	if quota.Scope == "project" {
		return "project " + quota.WorkspaceRoot
	}
	return "global"
}

func (d *Daemon) budgetAggregates(now time.Time) ([]BudgetAggregateView, error) {
	cfg, err := d.loadScopeBudgetConfig()
	if err != nil {
		return nil, err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.budgetAggregatesLocked(cfg.Quotas, now), nil
}

func (d *Daemon) budgetAggregatesLocked(quotas []ScopeBudgetQuota, now time.Time) []BudgetAggregateView {
	keys := map[string]ScopeBudgetQuota{
		"global\x00": {Scope: "global"},
	}
	for _, quota := range quotas {
		quota = normalizeScopeBudgetQuota(quota)
		if quota.Scope == "global" || quota.Scope == "project" {
			keys[quota.Scope+"\x00"+quota.WorkspaceRoot] = quota
		}
	}
	for _, entry := range d.registry {
		scope, root := entryScopeAndRoot(entry)
		if scope == "project" {
			key := "project\x00" + cleanWorkspaceRoot(root)
			if _, ok := keys[key]; !ok {
				keys[key] = ScopeBudgetQuota{Scope: "project", WorkspaceRoot: root}
			}
		}
	}
	views := make([]BudgetAggregateView, 0, len(keys))
	for _, quota := range keys {
		views = append(views, d.aggregateBudgetLocked(quota.Scope, quota.WorkspaceRoot, quotas, now))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Scope != views[j].Scope {
			return views[i].Scope < views[j].Scope
		}
		return views[i].WorkspaceRoot < views[j].WorkspaceRoot
	})
	return views
}

func (d *Daemon) aggregateBudgetLocked(scope, workspaceRoot string, quotas []ScopeBudgetQuota, now time.Time) BudgetAggregateView {
	scope = strings.TrimSpace(scope)
	workspaceRoot = cleanWorkspaceRoot(workspaceRoot)
	view := BudgetAggregateView{
		Scope:         scope,
		WorkspaceRoot: workspaceRoot,
	}
	for _, quota := range quotas {
		quota = normalizeScopeBudgetQuota(quota)
		if quota.Scope == scope && sameWorkspaceRoot(quota.WorkspaceRoot, workspaceRoot) {
			view.DailyModelCallLimit = quota.DailyModelCallLimit
			view.DailyModelCostLimit = quota.DailyModelCostLimit
			break
		}
	}
	window := budgetWindowStart(now.UTC())
	view.WindowStartedAt = &window
	for _, entry := range d.registry {
		if !entryMatchesAggregate(entry, scope, workspaceRoot) {
			continue
		}
		view.SessionCount++
		budget := entry.Runtime.Budget
		if !budget.WindowStartedAt.IsZero() && budget.WindowStartedAt.Equal(window) {
			view.DailyModelCalls += budget.DailyModelCalls
			view.DailyModelCost += budget.DailyModelCost
			if view.ModelCostCurrency == "" {
				view.ModelCostCurrency = budget.ModelCostCurrency
			}
		}
		if budget.LastBlockedReason != "" {
			view.LastBlockedReason = budget.LastBlockedReason
		}
	}
	view.Blocked = (view.DailyModelCallLimit > 0 && view.DailyModelCalls >= view.DailyModelCallLimit) ||
		(view.DailyModelCostLimit > 0 && view.DailyModelCost >= view.DailyModelCostLimit)
	return view
}

func entryMatchesAggregate(entry *SessionEntry, scope, workspaceRoot string) bool {
	if entry == nil {
		return false
	}
	if scope == "global" {
		return true
	}
	entryScope, entryRoot := entryScopeAndRoot(entry)
	return scope == "project" && entryScope == "project" && sameWorkspaceRoot(entryRoot, workspaceRoot)
}

func entryScopeAndRoot(entry *SessionEntry) (string, string) {
	if entry == nil {
		return "global", ""
	}
	meta, _, _ := agent.LoadBranchMeta(entry.Path)
	scope := meta.DefaultScope()
	root := firstNonEmpty(entry.Runtime.WorkspaceRoot, meta.WorkspaceRoot)
	if scope == "global" && strings.TrimSpace(root) != "" {
		scope = "project"
	}
	if scope == "project" {
		root = cleanWorkspaceRoot(root)
	} else {
		root = ""
	}
	return scope, root
}
