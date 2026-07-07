//go:build darwin

package main

/*
#include <stdint.h>
#include <dispatch/dispatch.h>

extern void reasonixDesktopMainHeartbeat(void);

static dispatch_source_t reasonix_main_heartbeat_timer;

static void reasonix_main_heartbeat_handler(void *ctx) {
	reasonixDesktopMainHeartbeat();
}

static void reasonix_start_main_heartbeat(uint64_t interval_ms) {
	if (reasonix_main_heartbeat_timer != NULL) {
		return;
	}
	reasonix_main_heartbeat_timer = dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER, 0, 0, dispatch_get_main_queue());
	dispatch_set_context(reasonix_main_heartbeat_timer, NULL);
	dispatch_source_set_event_handler_f(reasonix_main_heartbeat_timer, reasonix_main_heartbeat_handler);
	dispatch_source_set_timer(reasonix_main_heartbeat_timer, dispatch_time(DISPATCH_TIME_NOW, 0), interval_ms * NSEC_PER_MSEC, 100 * NSEC_PER_MSEC);
	dispatch_resume(reasonix_main_heartbeat_timer);
}

static void reasonix_stop_main_heartbeat(void) {
	if (reasonix_main_heartbeat_timer == NULL) {
		return;
	}
	dispatch_source_cancel(reasonix_main_heartbeat_timer);
	reasonix_main_heartbeat_timer = NULL;
}
*/
import "C"

import "time"

func mainThreadWatchdogSupported() bool {
	return true
}

func startNativeMainThreadHeartbeat(intervalMS uint64) {
	C.reasonix_start_main_heartbeat(C.uint64_t(intervalMS))
}

func stopNativeMainThreadHeartbeat() {
	C.reasonix_stop_main_heartbeat()
}

//export reasonixDesktopMainHeartbeat
func reasonixDesktopMainHeartbeat() {
	recordMainThreadHeartbeat(time.Now())
}
