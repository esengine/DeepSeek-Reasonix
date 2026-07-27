package main

import (
	"fmt"
	"time"

	"reasonix/internal/repair"
)

func (a *App) recordPreviousRunDiagnostics() {
	previous := a.previousRun
	if !previous.Abnormal {
		return
	}
	report := previousRunReport(previous)
	_ = writePendingReport(report, true)
	if m := a.metrics.Load(); m != nil {
		m.inc("desktop_exit", "abnormal")
		m.inc("desktop_exit_phase", metricBucket(previous.Phase))
		m.inc("desktop_uptime", metricBucket(previous.UptimeBucket))
		m.inc("desktop_install", metricBucket(previous.InstallProfile))
		if previous.UpdateTo != "" {
			m.inc("desktop_update_transition", "probationary")
		} else {
			m.inc("desktop_update_transition", "none")
		}
		m.persist()
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
	message := fmt.Sprintf(`[desktop.abnormal_exit]

Reasonix detected that the previous desktop process ended without a clean shutdown.

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
	report.Source = "native.lifecycle"
	report.Label = "desktop.abnormal_exit"
	report.ErrorType = "AbnormalDesktopExit"
	report.ErrorMessage = "The previous desktop process ended without recording a clean shutdown."
	report.TopFrame = "desktop.lifecycle." + phase
	report.FingerprintHint = "desktop.abnormal_exit." + phase
	report.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	report.Message = sanitizeCrashText(message, maxCrashDetailBytes)
	return report
}
