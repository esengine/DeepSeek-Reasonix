package tool

import "context"

// ShellLaunch is the final process description, after sandbox and environment
// preparation. Observers must not persist Arguments or Environment verbatim.
type ShellLaunch struct {
	Executable  string
	Interpreter string
	Directory   string
	Arguments   []string
	Environment []string
	Command     string
}

// ShellLaunchObserver is installed only around a host-selected tool execution.
// Finish receives physical completion, not merely return from a cancelled wait.
type ShellLaunchObserver interface {
	Begin(context.Context, ShellLaunch) (finish func(completed bool), err error)
}

type shellLaunchObserverKey struct{}
type shellLaunchObserverValue struct{ observer ShellLaunchObserver }

func WithShellLaunchObserver(ctx context.Context, observer ShellLaunchObserver) context.Context {
	return context.WithValue(ctx, shellLaunchObserverKey{}, shellLaunchObserverValue{observer})
}

func WithoutShellLaunchObserver(ctx context.Context) context.Context {
	return WithShellLaunchObserver(ctx, nil)
}

func ObserveShellLaunch(ctx context.Context, launch ShellLaunch) (func(bool), error) {
	if value, _ := ctx.Value(shellLaunchObserverKey{}).(shellLaunchObserverValue); value.observer != nil {
		return value.observer.Begin(ctx, launch)
	}
	return func(bool) {}, nil
}
