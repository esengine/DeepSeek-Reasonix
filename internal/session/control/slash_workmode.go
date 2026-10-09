package control

import (
	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/agentpreset"
	"reasonix/internal/contract/config"
)

// presetNotice answers /preset and its /work-mode and /profile aliases: no
// argument shows the role setting, a canonical name sets it for the session.
func (c *Controller) presetNotice(fields []string) {
	if fields[0] != "/preset" {
		c.notice(i18n.M.WorkModeDeprecatedNotice)
	}
	if len(fields) == 1 {
		c.notice(fmt.Sprintf(i18n.M.PresetCurrentFmt, agentpreset.Normalize(c.AgentPreset()), i18n.M.WorkModeUsage))
		return
	}
	if len(fields) > 2 || !agentpreset.IsValid(fields[1]) {
		c.notice(i18n.M.WorkModeUsage)
		return
	}
	c.SetAgentPreset(strings.ToLower(fields[1]))
	c.notice(fmt.Sprintf(i18n.M.PresetSetFmt, agentpreset.Normalize(fields[1])))
}

// remoteNotice lists the SSH hosts this machine's config declares. Connecting
// to one opens a workspace on that host, which a running session cannot do.
func (c *Controller) remoteNotice() {
	cfg, err := config.Load()
	if err != nil {
		c.notice("remote: " + err.Error())
		return
	}
	if len(cfg.Remote.Hosts) == 0 {
		c.notice(i18n.M.RemoteNoHostsHint)
		return
	}
	var b strings.Builder
	for _, h := range cfg.Remote.Hosts {
		target := h.Host
		if h.User != "" {
			target = h.User + "@" + target
		}
		if h.Port != 0 && h.Port != 22 {
			target = fmt.Sprintf("%s:%d", target, h.Port)
		}
		fmt.Fprintf(&b, "  %s  %s\n", h.Name, target)
	}
	b.WriteString(i18n.M.RemoteConnectHint)
	c.notice(b.String())
}
