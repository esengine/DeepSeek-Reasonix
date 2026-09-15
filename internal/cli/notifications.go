package cli

import (
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/notify"
)

var newNotificationSender = func() notify.Sender { return notify.NewPlatformSender() }
var newTerminalNotificationSender = func() notify.Sender { return notify.NewTerminalSender() }

func notificationSenderForConfig(cfg config.NotificationsConfig) notify.Sender {
	base := newNotificationSender()
	if cfg.TerminalBell {
		return notify.MultiSender{base, newTerminalNotificationSender()}
	}
	return base
}

// withNotifications adds system notifications to CLI event streams when configured.
func withNotifications(sink event.Sink, cfg *config.Config) event.Sink {
	if cfg == nil || !cfg.Notifications.Enabled {
		return sink
	}
	return notify.NewSink(sink, notificationSenderForConfig(cfg.Notifications), cfg.Notifications)
}
