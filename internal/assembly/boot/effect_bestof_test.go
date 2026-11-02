package boot

import (
	"reasonix/internal/contract/provider"
)

func hasToolResult(req provider.Request) bool {
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			return true
		}
	}
	return false
}
