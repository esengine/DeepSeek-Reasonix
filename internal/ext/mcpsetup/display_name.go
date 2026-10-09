package mcpsetup

import "reasonix/internal/base/textutil"

// DisplayName is a server name fit to print. A repository chooses the names in
// its .mcp.json, so one may carry escape sequences or bidi controls that would
// repaint a terminal or make the line read differently from what it says.
func DisplayName(name string) string { return textutil.SanitizeLaunch(name) }
