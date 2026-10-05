package control

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/agent"
)

// ProgressWatchSettings is the user's [progress_watch] section with its
// defaults resolved. Only the user file is read: a project file cannot set it.
type ProgressWatchSettings struct {
	Pause                bool   `json:"pause"`
	Rounds               int    `json:"rounds"`
	TokenMultiple        int    `json:"tokenMultiple"`
	DefaultRounds        int    `json:"defaultRounds"`
	DefaultTokenMultiple int    `json:"defaultTokenMultiple"`
	Path                 string `json:"path"`
}

// ProgressWatchFromConfig is the watch an executor runs under. The notice half
// is always on; the pause half is the user's switch.
func ProgressWatchFromConfig(cfg *config.Config) agent.ProgressWatch {
	return agent.ProgressWatch{
		Rounds:        cfg.ProgressWatchRounds(),
		TokenMultiple: cfg.ProgressWatchTokenMultiple(),
		Pause:         cfg.ProgressWatch.Pause,
	}
}

func (c *Controller) ProgressWatchSettings() ProgressWatchSettings {
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	return ProgressWatchSettings{
		Pause:                cfg.ProgressWatch.Pause,
		Rounds:               cfg.ProgressWatchRounds(),
		TokenMultiple:        cfg.ProgressWatchTokenMultiple(),
		DefaultRounds:        config.DefaultProgressWatchRounds,
		DefaultTokenMultiple: config.DefaultProgressWatchTokenMultiple,
		Path:                 path,
	}
}

// SaveProgressWatchSettings persists the section and hands it to the running
// executor, which reads it at each round boundary — no rebuild, so a turn that
// is stalling right now is the one the change applies to.
func (c *Controller) SaveProgressWatchSettings(in ProgressWatchSettings) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetProgressWatch(config.ProgressWatchConfig{
		Pause: in.Pause, Rounds: in.Rounds, TokenMultiple: in.TokenMultiple,
		// The settings screen does not edit the retry budget; carry the stored
		// value so saving the pause switch never clears it.
		PerseverationRetries: cfg.ProgressWatch.PerseverationRetries,
	}); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if c.executor != nil {
		c.executor.SetProgressWatch(ProgressWatchFromConfig(cfg))
	}
	return nil
}
