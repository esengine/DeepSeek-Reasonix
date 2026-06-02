package control

import (
	"errors"
	"fmt"

	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

// explainError maps a provider HTTP failure to an actionable, localized message
// so the turn-done error the UI shows is never a bare status code or silent
// failure. Unknown errors (and nil) pass through unchanged.
func explainError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *provider.APIError
	if errors.As(err, &apiErr) {
		if msg := i18n.M.ProviderStatusMessage(apiErr.Status); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	var authErr *provider.AuthError
	if errors.As(err, &authErr) {
		msg := i18n.M.ProviderStatusMessage(authErr.Status)
		if msg == "" {
			return err
		}
		if authErr.KeyEnv != "" {
			return fmt.Errorf("%s (%s)", msg, authErr.KeyEnv)
		}
		return errors.New(msg)
	}
	return err
}
