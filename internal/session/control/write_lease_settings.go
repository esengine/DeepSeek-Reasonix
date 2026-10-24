package control

import (
	"fmt"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/workspacelease"
)

// WriteLeaseSettings separates the saved user choice from the current runtime.
type WriteLeaseSettings struct {
	Optimistic bool `json:"optimistic"`
	// MaxWaitMinutes is how long a write waits behind another session's claim
	// before it fails; 0 waits as long as the turn lasts.
	MaxWaitMinutes int                       `json:"maxWaitMinutes"`
	Path           string                    `json:"path"`
	Collaboration  string                    `json:"collaboration,omitempty"`
	AutoIntegrate  bool                      `json:"autoIntegrate"`
	Effective      *WriteCoordinationRuntime `json:"effective,omitempty"`
}

type WriteCoordinationRuntime struct {
	Collaboration          string `json:"collaboration"`
	AutoIntegrate          bool   `json:"autoIntegrate"`
	Optimistic             bool   `json:"optimistic"`
	MaxWaitMinutes         int    `json:"maxWaitMinutes"`
	MaxSubagentConcurrency int    `json:"maxSubagentConcurrency"`
	MaxParallelWriters     int    `json:"maxParallelWriters"`
	TransactionWaitSeconds int    `json:"transactionWaitSeconds"`
	// AutomaticIsolationFallback marks auto runs whose writers share the workspace
	// because this host cannot bound an isolated candidate's writes.
	AutomaticIsolationFallback bool `json:"automaticIsolationFallback,omitempty"`
	configPath                 string
}

type WriteCoordinationEdit struct {
	Optimistic     *bool   `json:"optimistic,omitempty"`
	MaxWaitMinutes *int    `json:"maxWaitMinutes,omitempty"`
	Collaboration  *string `json:"collaboration,omitempty"`
	AutoIntegrate  *bool   `json:"autoIntegrate,omitempty"`
}

func cloneWriteCoordination(value *WriteCoordinationRuntime) *WriteCoordinationRuntime {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func WriteCoordinationFromConfig(cfg *config.Config, owner *workspacelease.Owner, scheduler *writeclaim.SubagentScheduler) *WriteCoordinationRuntime {
	mode, err := cfg.CollaborationMode()
	if err != nil {
		return nil
	}
	policy := owner.Policy()
	total, writers := scheduler.Limits()
	result := &WriteCoordinationRuntime{Collaboration: mode, Optimistic: policy.Optimistic,
		AutoIntegrate:  cfg.AutoIntegrationEnabled(),
		MaxWaitMinutes: int(policy.MaxWait / time.Minute), MaxSubagentConcurrency: total, MaxParallelWriters: writers,
		configPath: cfg.Roots().UserConfigPath()}
	if mode != "legacy" {
		result.TransactionWaitSeconds = int(mutation.DefaultWaitLimit / time.Second)
	}
	return result
}

// WriteLeaseSettings reads [agent] write_lease from the user file.
func (c *Controller) WriteLeaseSettings() WriteLeaseSettings {
	path := c.writeCoordinationConfigPath()
	cfg := config.LoadForEdit(path)
	mode, _ := cfg.CollaborationMode()
	result := WriteLeaseSettings{Optimistic: cfg.WriteLeaseOptimistic(), MaxWaitMinutes: int(cfg.WriteLeaseMaxWait() / time.Minute), Path: path, Collaboration: mode, AutoIntegrate: cfg.AutoIntegrationEnabled()}
	if c.coordination != nil {
		copy := *c.coordination
		result.Effective = &copy
	}
	return result
}

// SaveWriteLeaseSettings persists the choices given, nil leaving one as it is,
// to the user file. A session keeps the lease it was built with, and a rebuild
// adopts it, so a choice reaches the sessions opened after it, not the ones
// already running.
func (c *Controller) SaveWriteLeaseSettings(optimistic *bool, maxWaitMinutes *int) error {
	return c.SaveWriteCoordinationSettings(WriteCoordinationEdit{Optimistic: optimistic, MaxWaitMinutes: maxWaitMinutes})
}

func (c *Controller) SaveWriteCoordinationSettings(edit WriteCoordinationEdit) error {
	if edit.MaxWaitMinutes != nil && *edit.MaxWaitMinutes < 0 {
		return fmt.Errorf("maxWaitMinutes cannot be negative")
	}
	return c.editWriteCoordination(func(cfg *config.Config) error {
		if edit.AutoIntegrate != nil {
			value := *edit.AutoIntegrate
			cfg.Agent.AutoIntegrate = &value
		}
		if edit.Collaboration != nil {
			value := *edit.Collaboration
			if value == "legacy" {
				value = ""
			}
			if err := cfg.SetCollaborationMode(value); err != nil {
				return err
			}
		}
		if edit.Optimistic != nil {
			cfg.SetWriteLease(*edit.Optimistic)
		}
		if edit.MaxWaitMinutes != nil {
			cfg.SetWriteLeaseMaxWaitMinutes(*edit.MaxWaitMinutes)
		}
		return nil
	})
}

func (c *Controller) editWriteCoordination(edit func(*config.Config) error) error {
	path := c.writeCoordinationConfigPath()
	unlock, err := config.LockConfigFileEdits(path)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.LoadForEditReadOnlyStrict(path)
	if err != nil {
		return err
	}
	if err := edit(cfg); err != nil {
		return err
	}
	return cfg.SaveToScope(path, config.RenderScopeUser)
}

func (c *Controller) writeCoordinationConfigPath() string {
	if c.coordination != nil && c.coordination.configPath != "" {
		return c.coordination.configPath
	}
	return config.UserConfigPath()
}
