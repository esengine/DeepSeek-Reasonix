//go:build windows

package edge

import "sync"

type ProcessFailedDiagnostic struct {
	Kind                COREWEBVIEW2_PROCESS_FAILED_KIND
	Reason              COREWEBVIEW2_PROCESS_FAILED_REASON
	ReasonAvailable     bool
	ExitCode            int32
	ExitCodeAvailable   bool
	ProcessDescription  string
	FailureSourceModule string
	Recovery            string
}

var processFailedObserver struct {
	sync.RWMutex
	callback func(ProcessFailedDiagnostic)
}

// SetProcessFailedObserver installs Reasonix's process-level diagnostics hook.
// Browser-process events invoke it synchronously before the public Wails callback,
// because Wails terminates the host process from that callback.
func SetProcessFailedObserver(callback func(ProcessFailedDiagnostic)) {
	processFailedObserver.Lock()
	processFailedObserver.callback = callback
	processFailedObserver.Unlock()
}

func notifyProcessFailedObserver(diagnostic ProcessFailedDiagnostic) bool {
	processFailedObserver.RLock()
	callback := processFailedObserver.callback
	processFailedObserver.RUnlock()
	if callback == nil {
		return false
	}
	callback(diagnostic)
	return true
}

func collectProcessFailedDiagnostic(args *ICoreWebView2ProcessFailedEventArgs) ProcessFailedDiagnostic {
	diagnostic := ProcessFailedDiagnostic{
		Kind:     COREWEBVIEW2_PROCESS_FAILED_KIND_UNKNOWN_PROCESS_EXITED,
		Recovery: "not_applicable",
	}
	if kind, err := args.GetProcessFailedKind(); err == nil {
		diagnostic.Kind = kind
	}
	args2, err := args.GetICoreWebView2ProcessFailedEventArgs2()
	if err == nil && args2 != nil {
		defer args2.Release()
		if reason, reasonErr := args2.GetReason(); reasonErr == nil {
			diagnostic.Reason = reason
			diagnostic.ReasonAvailable = true
		}
		if exitCode, exitErr := args2.GetExitCode(); exitErr == nil {
			diagnostic.ExitCode = exitCode
			diagnostic.ExitCodeAvailable = true
		}
		if description, descriptionErr := args2.GetProcessDescription(); descriptionErr == nil {
			diagnostic.ProcessDescription = description
		}
	}
	args3, err := args.GetICoreWebView2ProcessFailedEventArgs3()
	if err == nil && args3 != nil {
		defer args3.Release()
		if module, moduleErr := args3.GetFailureSourceModulePath(); moduleErr == nil {
			diagnostic.FailureSourceModule = module
		}
	}
	return diagnostic
}
