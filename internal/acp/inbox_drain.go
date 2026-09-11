package acp

import (
	"context"
	"errors"

	"reasonix/internal/sessioninbox"
)

type acpInboxController interface {
	InboxSnapshot() sessioninbox.InboxSnapshot
	RunInboxTurn(context.Context, string) error
}

func drainACPInbox(ctx context.Context, ctrl acpController, runErr error) error {
	if runErr != nil {
		return runErr
	}
	inbox, ok := ctrl.(acpInboxController)
	if !ok {
		return nil
	}
	for {
		snap := inbox.InboxSnapshot()
		if snap.Paused {
			return nil
		}
		nextID := ""
		for _, item := range snap.Items {
			if item.State == sessioninbox.StateQueued {
				nextID = item.ID
				break
			}
		}
		if nextID == "" {
			return nil
		}
		err := inbox.RunInboxTurn(ctx, nextID)
		if errors.Is(err, sessioninbox.ErrNotFound) || errors.Is(err, sessioninbox.ErrInvalidState) {
			// A concurrent queue edit won the claim; refresh the FIFO snapshot.
			continue
		}
		if err != nil {
			return err
		}
	}
}
