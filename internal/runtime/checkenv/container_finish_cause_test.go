//go:build !windows

package checkenv

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"testing"
)

func TestContainerFinishNamesWhyTheRunIsNotEvidence(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*containerEnvironment)
		want  string
	}{
		{"cancelled", func(i *containerEnvironment) { i.cancelled = true }, "controlled_execution_cancelled"},
		{"scope", func(i *containerEnvironment) { i.scopeErr = errors.New("scope unconfirmed") }, "controlled_scope_unconfirmed"},
		{"exit", func(i *containerEnvironment) { i.exitCode = 137 }, "controlled_exit_status_changed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command("true")
			if err := cmd.Run(); err != nil {
				t.Skip(err)
			}
			i := &containerEnvironment{cmd: cmd, ctx: context.Background(), started: true, exitCode: 0}
			c.setup(i)
			i.finish()
			if !slices.Equal(i.input.Gaps, []string{c.want}) {
				t.Fatalf("gaps = %v, want %s", i.input.Gaps, c.want)
			}
		})
	}
}
