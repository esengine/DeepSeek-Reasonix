#ifndef REASONIX_WEBKIT_DIAGNOSTICS_LINUX_H
#define REASONIX_WEBKIT_DIAGNOSTICS_LINUX_H

void reasonix_install_webkit_observer(void);
extern void reasonixWebKitRuntimeReady(int major, int minor, int micro, int gpu_mode);
extern void reasonixWebKitProcessTerminated(int reason, int recovery, unsigned long long generation);

#ifdef REASONIX_WEBKIT_SMOKE
int reasonix_test_webkit_run(int mode);
void reasonix_test_webkit_event_seen(int reason, int recovery);
int reasonix_test_webkit_reload_count(void);
#endif

#endif
