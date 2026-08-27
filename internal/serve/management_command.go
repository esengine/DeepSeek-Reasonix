package serve

import "strings"

func isServeManagementCommand(input string) bool {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "/compact", "/context", "/new", "/clear", "/goal", "/memory", "/remember",
		"/migrate", "/migration", "/skill", "/skills", "/plugin", "/plugins",
		"/reload-cmd", "/hooks", "/mcp", "/provider", "/tree", "/branch",
		"/switch", "/rewind":
		return true
	default:
		return false
	}
}
