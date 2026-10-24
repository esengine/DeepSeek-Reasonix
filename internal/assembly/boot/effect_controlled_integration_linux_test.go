//go:build linux

package boot

import (
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/runtime/checkenv"
)

func TestEffectControlledChecksAutomaticallyIntegrateWithoutModelApply(t *testing.T) {
	controlledAutoIntegrationEffect(t, testenv.VerificationRuntime(t), checkenv.ClientSpec{})
}
