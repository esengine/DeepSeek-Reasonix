package control

import (
	"context"
	"errors"
	"strings"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/model/providerhealth"
)

type ProviderHealthReading = providerhealth.Reading

type ProviderHealthSettings struct {
	Enabled bool `json:"enabled"`
}

func (c *Controller) ProviderHealthSettings() (ProviderHealthSettings, error) {
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		return ProviderHealthSettings{}, err
	}
	return ProviderHealthSettings{Enabled: cfg.Desktop.ProviderHealthChecks}, nil
}

func (c *Controller) SaveProviderHealthSettings(enabled bool) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		return err
	}
	cfg.Desktop.ProviderHealthChecks = enabled
	return cfg.SaveTo(config.UserConfigPath())
}

var ErrProviderHealthNotFound = errors.New("provider health: saved provider not found")

// ProviderHealth checks only saved endpoints, with no completion or configuration writes.
func (c *Controller) ProviderHealth(ctx context.Context, name string) (ProviderHealthReading, error) {
	cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
	if err != nil {
		return ProviderHealthReading{}, err
	}
	if !cfg.Desktop.ProviderHealthChecks {
		return ProviderHealthReading{Connection: "disabled", Wallet: "unknown"}, nil
	}
	name = strings.TrimSpace(name)
	p, ok := cfg.Provider(name)
	if !ok || !cfg.ProviderAdded(name) {
		return ProviderHealthReading{}, ErrProviderHealthNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return c.providerHealth.Read(ctx, p, cfg.NetworkProxySpec()), nil
}
