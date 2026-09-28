package config

import (
	"errors"
	"fmt"
)

// ErrProjectProgramChanged is an approved program whose files changed after
// the approval was checked, so the host refuses to run it.
var ErrProjectProgramChanged = errors.New("workspace program changed since it was approved")

// Verify reports ErrProjectProgramChanged when a file the approval covered no
// longer holds the approved content.
func (p ProjectProgram) Verify() error {
	if len(p.Files) == 0 || p.currentDigest() == p.Digest {
		return nil
	}
	return fmt.Errorf("%w: %s %q; approve it again with `reasonix trust`", ErrProjectProgramChanged, p.Kind, p.Name)
}

// ProjectProgramVerifier returns the check an executor runs before starting the
// project-declared program of kind and name, or nil when the program in force
// is not one a workspace declared.
func (c *Config) ProjectProgramVerifier(kind ProjectProgramKind, name string) func() error {
	if c == nil {
		return nil
	}
	for _, p := range c.projectScope.admitted {
		if p.Kind == kind && p.Name == name && len(p.Files) > 0 {
			return p.Verify
		}
	}
	return nil
}
