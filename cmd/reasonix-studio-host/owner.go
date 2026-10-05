package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	goruntime "runtime"
	"sync"
)

// The acts a handover asks of the shell. Only two travel: releasing the
// application is this process's own hub, and the shell has nothing to do for it.
const (
	actRelaunch = "relaunch"
	actQuit     = "quit"
)

// shellOwner is the application this process was spawned by, reached over the
// pipe the handshake went out on -- the only channel pointing this way, since
// stdin is the lease and HTTP runs from the shell to here. Nothing is waited
// for: both acts are ones the shell performs by ending, so an acknowledgement
// would have to come from a process on its way out.
type shellOwner struct {
	mu sync.Mutex
	to io.Writer
}

// PrepareForUpdate has nothing to do here, and that is the shape rather than an
// omission. What holds the application open is the shell and this process
// inside it; nothing either can do short of ending releases that, and ending is
// the third act. Under a shell that is its own process the two are separable,
// which is why the interface keeps them apart.
func (o *shellOwner) PrepareForUpdate(context.Context) error { return nil }

// RelaunchAfterUpdate arms the shell's own restart, and only where nothing else
// is waiting to start the successor. Windows runs the next installer and macOS
// leaves a helper holding the swap; arming a restart there would start a second
// application beside the one those bring up.
func (o *shellOwner) RelaunchAfterUpdate(context.Context) error {
	if goruntime.GOOS != "linux" {
		return nil
	}
	return o.ask(actRelaunch)
}

// EndApplication ends the shell, which is the application. This process is its
// child and drains when the lease closes behind it, so it does not end itself.
func (o *shellOwner) EndApplication(context.Context) {
	_ = o.ask(actQuit)
}

// ask writes one line. The lock is not for the writer but for the line: two
// acts interleaved inside one write would reach the parent as neither.
func (o *shellOwner) ask(act string) error {
	body, err := json.Marshal(struct {
		Act string `json:"act"`
	}{act})
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, err := fmt.Fprintf(o.to, "%s\n", body); err != nil {
		return fmt.Errorf("ask the shell to %s: %w", act, err)
	}
	return nil
}
