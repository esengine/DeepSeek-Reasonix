// Package hostenv captures process-owned launch inputs at frontend boundaries.
package hostenv

import (
	"os"
	"os/exec"

	"reasonix/internal/runtime/checkenv"
)

func VerificationClient() checkenv.ClientSpec {
	executable, _ := exec.LookPath("docker")
	var environment []string
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "PATH", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "SystemRoot"} {
		if value, ok := os.LookupEnv(key); ok {
			environment = append(environment, key+"="+value)
		}
	}
	return checkenv.ClientSpec{Executable: executable, Environment: environment}
}
