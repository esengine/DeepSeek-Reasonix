package boot

import (
	"reasonix/internal/agent"
	"reasonix/internal/history"
)

func newObservedSession(systemPrompt string) *agent.Session {
	session := agent.NewSession(systemPrompt)
	session.SetPersistObserver(history.PersistObserver())
	return session
}
