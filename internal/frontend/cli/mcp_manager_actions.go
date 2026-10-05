package cli

// mcp_manager_actions.go applies /mcp manager actions: connect, disable, remove,
// mode, auth, and config editing.

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func mcpOpenCommand(target string) (*exec.Cmd, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("empty target")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target), nil
	default:
		return exec.Command("xdg-open", target), nil
	}
}
