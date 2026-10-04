package serve

import (
	"errors"
	"strconv"

	"reasonix/internal/contract/config"
)

// ErrSharePortInUse is a configured share port another process already holds.
var ErrSharePortInUse = errors.New("the share port is already in use")

// RestorePort sets the port from the user config at startup. An out-of-range
// value reads as unset, as Config.SharePort does.
func (s *DeviceShare) RestorePort(port int) {
	if config.ValidateSharePort(port) != nil {
		port = 0
	}
	s.mu.Lock()
	s.port = port
	s.mu.Unlock()
}

// SetPort saves the port to the user config and uses it from the next Open;
// zero goes back to a system-picked port. Nothing changes when saving fails.
func (s *DeviceShare) SetPort(port int) error {
	if err := config.ValidateSharePort(port); err != nil {
		return err
	}
	if err := s.persistPort(port); err != nil {
		return err
	}
	s.RestorePort(port)
	return nil
}

func persistSharePort(port int) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetSharePort(port); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

func (s *DeviceShare) listenPort() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strconv.Itoa(s.port)
}
