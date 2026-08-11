package main

import (
	"fmt"
	"runtime"
	"time"

	"reasonix/internal/repair"
)

func (a *App) recordPreviousRunDiagnostics() {
	previous := a.lifecycle.previousRun
	if previous.Abnormal {
		report := previousRunReport(previous)
		_ = writePendingReport(report, true)
		if m := a.metrics.Load(); m != nil {
			m.inc("desktop_legacy_exit", "abnormal")
			m.inc("desktop_legacy_exit_phase", metricBucket(previous.Phase))
			m.persist()
		}
	}
	for _, lifecycle := range a.lifecycle.previousRuns {
		_ = writePendingReport(desktopLifecycleReport(lifecycle), true)
		if m := a.metrics.Load(); m != nil {
			m.inc("desktop_exit", "abnormal")
			m.inc("desktop_exit_phase", metricBucket(lifecycle.Phase))
			m.persist()
		}
	}
}

func previousRunReport(previous repair.PreviousRunObservation) crashReport {
	phase := metricBucket(previous.Phase)
	uptime := metricBucket(previous.UptimeBucket)
	profile := metricBucket(previous.InstallProfile)
	update := "none"
	if previous.UpdateTo != "" {
		update = sanitizeCrashField(previous.UpdateFrom+" -> "+previous.UpdateTo, 128)
	}
	message := fmt.Sprintf(`[desktop.legacy_abnormal_exit]

Reasonix consumed a legacy v1.18-v1.19 startup record whose owner was no longer running.

--- lifecycle context ---
phase: %s
uptime bucket: %s
install profile: %s
previous version: %s
update transition: %s`,
		phase,
		uptime,
		profile,
		sanitizeCrashField(previous.Version, 64),
		update,
	)
	report := baseCrashReport("crash")
	report.SchemaVersion = 2
	report.Source = "native.lifecycle.legacy"
	report.Label = "desktop.legacy_abnormal_exit"
	report.ErrorType = "LegacyAbnormalDesktopExit"
	report.ErrorMessage = "A legacy startup record was consumed once after its owner stopped."
	report.TopFrame = "desktop.lifecycle.legacy." + phase
	report.FingerprintHint = "desktop.legacy_abnormal_exit." + runtime.GOOS + "." + phase
	report.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	report.Message = sanitizeCrashText(message, maxCrashDetailBytes)
	return report
}

func desktopLifecycleReport(previous desktopLifecycleObservation) crashReport {
	phase := metricBucket(previous.Phase)
	message := fmt.Sprintf(`[desktop.abnormal_exit.v2]

Reasonix found a per-process lifecycle record whose desktop process was no longer running.

--- lifecycle context ---
phase: %s
previous version: %s
previous channel: %s
started at: %s
last phase update: %s`,
		phase,
		sanitizeCrashField(previous.Version, 64),
		sanitizeCrashField(previous.Channel, 32),
		sanitizeCrashField(previous.StartedAt, 64),
		sanitizeCrashField(previous.UpdatedAt, 64),
	)
	report := baseCrashReport("crash")
	report.SchemaVersion = 3
	report.Source = "native.lifecycle"
	report.Label = "desktop.abnormal_exit.v2"
	report.ErrorType = "AbnormalDesktopExit"
	report.ErrorMessage = "A per-process lifecycle record remained after its desktop process stopped."
	report.TopFrame = "desktop.lifecycle.v2." + phase
	report.FingerprintHint = "desktop.abnormal_exit.v2." + runtime.GOOS + "." + phase
	report.OccurredAt = sanitizeCrashField(previous.UpdatedAt, 64)
	report.Message = sanitizeCrashText(message, maxCrashDetailBytes)
	return report
}
