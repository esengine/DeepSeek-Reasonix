package egress

import (
	"context"
	"os/exec"
	"time"
)

func systemProxies() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return nil
	}
	return parseScutilProxies(string(out))
}
