package control

import (
	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
)

func remoteHostsText() string {
	cfg, err := config.Load()
	if err != nil {
		return err.Error()
	}
	if len(cfg.Remote.Hosts) == 0 {
		return i18n.M.RemoteNoHostsHint
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
		fmt.Fprintf(&b, "  · %s  %s\n", h.Name, target)
	}
	b.WriteString("  run `reasonix remote connect <name>` in a terminal to open the remote workspace")
	return b.String()
}
