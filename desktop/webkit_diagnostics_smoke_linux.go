//go:build linux && cgo && reasonix_webkit_smoke

package main

/*
#cgo CFLAGS: -DREASONIX_WEBKIT_SMOKE
#cgo !webkit2_41 pkg-config: gtk+-3.0 webkit2gtk-4.0
#cgo webkit2_41 pkg-config: gtk+-3.0 webkit2gtk-4.1
#include "webkit_diagnostics_linux.h"
*/
import "C"

const (
	webKitSmokeSuccess  = 1
	webKitSmokeFailure  = 2
	webKitSmokeTimeout  = 3
	webKitSmokeCooldown = 4
)

var webKitSmokeEvents = make(chan webKitNativeEvent, 4)

func init() {
	observeWebKitNativeEvent = func(event webKitNativeEvent) {
		select {
		case webKitSmokeEvents <- event:
		default:
		}
		C.reasonix_test_webkit_event_seen(C.int(event.reason), C.int(event.recovery))
	}
}

func runWebKitNativeSmoke(mode int) (int, []webKitNativeEvent, int) {
	for {
		select {
		case <-webKitSmokeEvents:
		default:
			goto drained
		}
	}
drained:
	result := int(C.reasonix_test_webkit_run(C.int(mode)))
	events := make([]webKitNativeEvent, 0, len(webKitSmokeEvents))
	for {
		select {
		case event := <-webKitSmokeEvents:
			events = append(events, event)
		default:
			return result, events, int(C.reasonix_test_webkit_reload_count())
		}
	}
}
