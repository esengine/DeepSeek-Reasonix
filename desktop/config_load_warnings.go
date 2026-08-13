package main

const configLoadWarningsEvent = "config:load-warnings"

func (a *App) nextConfigLoadWarningsRevision() uint64 {
	if a == nil {
		return 0
	}
	return a.runtimeEvents.configWarningsRevision.Add(1)
}

func (a *App) configLoadWarningsHandler() func([]string) bool {
	if a == nil {
		return nil
	}
	revision := a.nextConfigLoadWarningsRevision()
	return func(warnings []string) bool {
		return a.emitConfigLoadWarnings(revision, warnings)
	}
}

// emitConfigLoadWarnings transfers ownership of resilient-loader diagnostics
// to the persistent desktop banner. Returning false keeps boot's diagnostic
// notice when no Wails event context is available.
func (a *App) emitConfigLoadWarnings(revision uint64, warnings []string) bool {
	if a == nil || a.ctx == nil || len(warnings) == 0 {
		return false
	}
	owned := append([]string(nil), warnings...)
	a.runtimeEvents.Emit(a.ctx, configLoadWarningsEvent, owned, revision)
	return true
}
