//go:build !linux

package sandbox

import (
	"fmt"
	"os"
)

type capabilityActivationWitness struct{}

func newCapabilityActivationWitness() (*capabilityActivationWitness, *os.File, error) {
	return nil, nil, fmt.Errorf("capability activation witnesses are supported only on Linux")
}

func (*capabilityActivationWitness) state(CapabilityExecutionOutcome) CapabilityAppliedState {
	return CapabilityApplicationUnknown
}
func (*capabilityActivationWitness) close() {}
