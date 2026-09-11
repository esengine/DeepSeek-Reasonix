package main

import "reasonix/internal/event"

// persistMetricsEvent snapshots controller-owned recovery counters after
// display persistence has acknowledged the final projection.
func persistMetricsEvent(app *App, metrics *metricsAggregator, tabID string, e event.Event) {
	if !metricsEventRequiresPersist(e) {
		return
	}
	if e.Kind == event.TurnDone {
		if tab := app.tabByID(tabID); tab != nil && tab.Ctrl != nil {
			observeControllerRecoveryMetrics(metrics, tab.Ctrl)
			observeControllerTurnEventMetrics(metrics, tab.Ctrl)
		}
	}
	metrics.persist()
}
