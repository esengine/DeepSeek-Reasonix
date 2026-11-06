package control

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"reasonix/internal/state/memory"
)

// ErrNoSuchSuggestion is returned when a suggestion was already decided or never existed.
var ErrNoSuchSuggestion = errors.New("memory.suggestion_not_found")

// MemorySuggestion is a fact the model wants to keep that waits for the user.
type MemorySuggestion struct {
	ID     string
	Memory memory.Memory
	At     time.Time
}

// suggestionBook is the ask-first queue. It has its own lock and never calls
// back into the Controller.
type suggestionBook struct {
	mu    sync.Mutex
	ask   bool
	seq   int
	items []MemorySuggestion
}

func (b *suggestionBook) add(m memory.Memory) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	id := "s" + strconv.Itoa(b.seq)
	b.items = append(b.items, MemorySuggestion{ID: id, Memory: m, At: time.Now()})
	return id
}

func (b *suggestionBook) list() []MemorySuggestion {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]MemorySuggestion(nil), b.items...)
}

func (b *suggestionBook) take(id string) (MemorySuggestion, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.items {
		if s.ID == id {
			b.items = append(b.items[:i], b.items[i+1:]...)
			return s, true
		}
	}
	return MemorySuggestion{}, false
}

func (b *suggestionBook) restore(s MemorySuggestion) {
	b.mu.Lock()
	b.items = append([]MemorySuggestion{s}, b.items...)
	b.mu.Unlock()
}

func (b *suggestionBook) asking() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ask
}

// SuggestMemory implements memory.Suggester: in ask mode the fact is held for
// the user instead of written.
func (c *Controller) SuggestMemory(m memory.Memory) (string, bool) {
	if !c.asksBeforeRemembering() || c.memory.current() == nil {
		return "", false
	}
	return c.memory.book.add(m), true
}

// MemorySuggestions lists the facts waiting for a decision, oldest first.
func (c *Controller) MemorySuggestions() []MemorySuggestion {
	return c.memory.book.list()
}

// AcceptMemorySuggestion saves a held fact, with the user's edit when given,
// and returns the file written.
func (c *Controller) AcceptMemorySuggestion(id string, edited *memory.Memory) (string, error) {
	s, ok := c.memory.book.take(id)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoSuchSuggestion, id)
	}
	fact := s.Memory
	if edited != nil {
		fact = *edited
		if fact.Name == "" {
			fact.Name = s.Memory.Name
		}
	}
	path, err := c.memory.saveMemory(fact)
	if err != nil {
		c.memory.book.restore(s)
	}
	return path, err
}

// DismissMemorySuggestion drops a held fact without saving it.
func (c *Controller) DismissMemorySuggestion(id string) error {
	if _, ok := c.memory.book.take(id); !ok {
		return fmt.Errorf("%w: %s", ErrNoSuchSuggestion, id)
	}
	return nil
}

func (b *suggestionBook) setAsk(ask bool) {
	b.mu.Lock()
	b.ask = ask
	b.mu.Unlock()
}

// asksBeforeRemembering is false under YOLO: a user who turned approvals off
// has already said the model may act, so its facts are saved, not held.
func (c *Controller) asksBeforeRemembering() bool {
	return c.memory.book.asking() && c.ToolApprovalMode() != ToolApprovalYolo
}
