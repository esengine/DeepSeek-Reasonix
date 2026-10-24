package checkenv

import (
	"io"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

type Environment interface {
	tool.ControlledShell
	io.Writer
	Inputs() []evidence.CheckInputs
	CapturedOutcome() evidence.CriterionResult
	Close()
}

func NewEnvironment(spec Spec, command, executionID, callID string) Environment {
	if strings.HasPrefix(spec.RuntimeRoot, "docker-image://") {
		return newContainerEnvironment(spec, command, executionID, callID)
	}
	return New(spec, command, executionID, callID)
}
