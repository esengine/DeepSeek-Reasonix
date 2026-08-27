package main

import (
	"encoding/json"
	"strings"
	"sync"
)

// eventLog records every emitRemoteEvent call from any goroutine.
type eventLog struct {
	mu     sync.Mutex
	events []string // "name payload"
}

func (l *eventLog) add(name string, payload any) {
	text, _ := json.Marshal(payload)
	l.mu.Lock()
	l.events = append(l.events, name+" "+string(text))
	l.mu.Unlock()
}

func (l *eventLog) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.events))
	copy(out, l.events)
	return out
}

func (l *eventLog) count(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}
