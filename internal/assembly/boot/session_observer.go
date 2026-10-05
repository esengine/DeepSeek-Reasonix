package boot

import (
	"reasonix/internal/state/history"
	"reasonix/internal/state/sessionstore"
)

func newObservedSession(systemPrompt string) *sessionstore.Session {
	session := sessionstore.NewSession(systemPrompt)
	session.SetPersistObserver(history.PersistObserver())
	return session
}
