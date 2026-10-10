// extension_notice.go — an extension left out of an operation, as a notice.
package boot

import (
	"fmt"

	"reasonix/internal/contract/event"
)

// extensionSkippedEvent is the typed notice for an optional extension whose
// sidecar is not running. Detail is the payload frontends word the sentence
// from; Text is the English fallback for a sink that cannot.
func extensionSkippedEvent(pluginID, point string) event.Event {
	return event.Event{
		Level: event.LevelWarn,
		Code:  event.NoticeCodeExtensionSkipped,
		Text: fmt.Sprintf("Extension %s's sidecar is not running, so it was skipped at %s. Start it under Tools & Integrations, or disable the extension.",
			pluginID, point),
		Detail: event.ExtensionSkipped{Extension: pluginID, Point: point, Reason: event.ExtensionSkipReasonNoLiveSidecar}.Encode(),
	}
}
