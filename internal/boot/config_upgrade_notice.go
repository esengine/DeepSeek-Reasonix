package boot

import (
	"reasonix/internal/config"
	"reasonix/internal/event"
)

func emitUserConfigUpgradeNotice(sink event.Sink, cfg *config.Config, deepSeekProtocolMigrated bool, deepSeekProtocolMigErr error) {
	if deepSeekProtocolMigrated {
		detail := cfg.OpenCodeGoUpgradeSummary()
		if detail == "" {
			detail = "Legacy built-in DeepSeek defaults now use Chat Completions with independent web search."
		}
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelInfo,
			Text:   "User configuration was upgraded.",
			Detail: detail + " Protocol changes start a new provider cache prefix; later requests rebuild normal prefix-cache reuse.",
		})
	} else if deepSeekProtocolMigErr != nil {
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelWarn,
			Text:   "DeepSeek protocol migration did not complete.",
			Detail: deepSeekProtocolMigErr.Error(),
		})
	}
}
