package agentpreset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/agentcomposition"
)

// RetainBinding records a binding assembled from locally authorized sources.
func (s *Store) RetainBinding(binding *agentcomposition.Binding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.trustedDirectory(), "bindings", binding.Digest+".json"), binding)
}

// VerifyBinding never treats self-authenticated imported history as executable authority.
func (s *Store) VerifyBinding(binding *agentcomposition.Binding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if len(binding.Digest) != 64 || strings.Trim(binding.Digest, "0123456789abcdef") != "" {
		return agentcomposition.ErrInvalid
	}
	data, err := os.ReadFile(filepath.Join(s.trustedDirectory(), "bindings", binding.Digest+".json"))
	if err != nil {
		return fmt.Errorf("%w: binding has no local authorization record", agentcomposition.ErrUnavailable)
	}
	var retained agentcomposition.Binding
	if err := json.Unmarshal(data, &retained); err != nil {
		return err
	}
	if err := retained.Validate(); err != nil {
		return err
	}
	if retained.Digest != binding.Digest {
		return agentcomposition.ErrInvalid
	}
	return nil
}
