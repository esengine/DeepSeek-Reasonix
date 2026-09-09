package imageinput

import (
	"context"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Config is frozen by boot and shared by independent per-session services.
type Config struct {
	Model   string
	Resolve func(string) (provider.Provider, error)
	Select  func(string, string) (string, bool)
}
type Service struct {
	config Config
	once   sync.Once
	queue  chan struct{}
	cached map[string]*provider.VisionSummary
}

func New(config Config) *Service {
	config.Model = strings.TrimSpace(config.Model)
	return &Service{config: config}
}

// Understand serializes a session's image prepasses without holding session locks.
func (s *Service) Understand(ctx context.Context, current string, images []string, history func() []provider.Message, sink event.Sink) (*provider.VisionSummary, error) {
	target, err := s.selectModel(current, images)
	if err != nil {
		return nil, err
	}
	if sink == nil {
		sink = event.Discard
	}
	s.once.Do(func() { s.queue = make(chan struct{}, 1) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case s.queue <- struct{}{}:
	}
	defer func() { <-s.queue }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	digests, key, cacheable, cached := s.lookup(target, images, history)
	if cached != nil {
		return cached, nil
	}
	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "正在分析图片…"})
	summary, err := s.summarizeImages(ctx, target, images, digests, sink)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cacheable {
		if s.cached == nil || len(s.cached) >= 32 {
			s.cached = make(map[string]*provider.VisionSummary)
		}
		s.cached[key] = clone(summary)
	}
	return summary, nil
}
