package boot

import (
	"os"
	"os/exec"
	"testing"

	"reasonix/internal/runtime/checkenv"
)

func TestEffectContainerControlledChecksAutomaticallyIntegrateWithoutModelApply(t *testing.T) {
	image := os.Getenv("REASONIX_TEST_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("requires a locally provisioned immutable verification image and Docker Engine")
	}
	executable, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	var environment []string
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "PATH", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "SystemRoot"} {
		if value, ok := os.LookupEnv(key); ok {
			environment = append(environment, key+"="+value)
		}
	}
	controlledAutoIntegrationEffect(t, "docker-image://"+image, checkenv.ClientSpec{Executable: executable, Environment: environment})
}
