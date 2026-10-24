//go:build !linux

package checkenv

import (
	"context"
	"os/exec"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

type Invocation struct{}

func New(Spec, string, string, string) *Invocation            { return &Invocation{} }
func (*Invocation) Inputs() []evidence.CheckInputs            { return nil }
func (*Invocation) Close()                                    {}
func (*Invocation) Started(*exec.Cmd)                         {}
func (*Invocation) Cancel() error                             { return ErrUnsupported }
func (*Invocation) WaitScope() error                          { return ErrUnsupported }
func (*Invocation) Write(body []byte) (int, error)            { return len(body), nil }
func (*Invocation) CapturedOutcome() evidence.CriterionResult { return evidence.CriterionUnavailable }

func (*Invocation) Prepare(context.Context, tool.ControlledShellRequest) ([]string, error) {
	return nil, ErrUnsupported
}
func (*Invocation) Command(context.Context, tool.ShellLaunch) (*exec.Cmd, func(), error) {
	return nil, nil, ErrUnsupported
}
