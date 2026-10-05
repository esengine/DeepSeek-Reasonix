package notify

import (
	"sync/atomic"

	"reasonix/internal/contract/config"
)

// Settings is what a sink reads on every event, so a window flipping the switch
// reaches the next turn rather than the next launch. One holder is shared by
// every runtime a host builds: the setting belongs to the machine, not to a
// session, and a second copy is a second answer.
type Settings struct {
	held atomic.Pointer[config.NotificationsConfig]
}

// NewSettings starts a holder at what the config said when it was read.
func NewSettings(cfg config.NotificationsConfig) *Settings {
	s := &Settings{}
	s.Store(cfg)
	return s
}

// Store replaces what every sink reading this holder will answer to next.
func (s *Settings) Store(cfg config.NotificationsConfig) {
	if s == nil {
		return
	}
	s.held.Store(&cfg)
}

// Load answers the current setting. A nil holder notifies about nothing, which
// is what a runtime built without one is asking for.
func (s *Settings) Load() config.NotificationsConfig {
	if s == nil {
		return config.NotificationsConfig{}
	}
	if cfg := s.held.Load(); cfg != nil {
		return *cfg
	}
	return config.NotificationsConfig{}
}
