package cli

import (
	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/notify"
)

var newNotificationSender = func() notify.Sender { return notify.NewPlatformSender() }

// withNotifications adds system notifications to CLI event streams when configured.
// The CLI reads its config once per process, so the holder never moves after it
// is made; a window that can flip the switch mid-run stores into its own.
func withNotifications(sink event.Sink, cfg *config.Config) event.Sink {
	if cfg == nil || !cfg.Notifications.Enabled {
		return sink
	}
	return notify.NewSink(sink, newNotificationSender(), i18n.M, notify.NewSettings(cfg.Notifications))
}
