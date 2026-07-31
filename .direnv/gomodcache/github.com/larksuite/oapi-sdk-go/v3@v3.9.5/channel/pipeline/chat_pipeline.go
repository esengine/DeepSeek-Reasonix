package pipeline

import (
	"context"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"strings"
	"sync"
	"time"
)

type FlushHandler func(context.Context, *types.BatchedDispatch) error

// ChatPipeline manages debouncing, batching, and serializing tasks for a single scope.
type ChatPipeline struct {
	mu             sync.Mutex
	config         types.BatchConfig
	serialOnly     bool
	buffer         []*types.NormalizedMessage
	bufferChars    int
	timer          *time.Timer
	pendingHandler FlushHandler

	// Task queue for serialization
	tasks chan func()

	stopCh chan struct{}
	closed bool
}

func NewChatPipeline(config types.BatchConfig, serialOnly bool) *ChatPipeline {
	cp := &ChatPipeline{
		config:     config,
		serialOnly: serialOnly,
		tasks:      make(chan func(), 100),
		stopCh:     make(chan struct{}),
	}
	go cp.worker()
	return cp
}

func (cp *ChatPipeline) worker() {
	for {
		select {
		case task, ok := <-cp.tasks:
			if !ok {
				return
			}
			task()
		case <-cp.stopCh:
			return
		}
	}
}

func (cp *ChatPipeline) Push(ctx context.Context, msg *types.NormalizedMessage, handler FlushHandler) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	cp.buffer = append(cp.buffer, msg)
	cp.bufferChars += len(msg.Content)
	if cp.pendingHandler == nil {
		cp.pendingHandler = handler
	}

	if len(cp.buffer) >= cp.config.MaxMessages || cp.bufferChars >= cp.config.MaxChars {
		cp.clearTimer()
		cp.enqueueFlush(ctx)
		return
	}

	if cp.config.DelayMs <= 0 || cp.serialOnly {
		cp.clearTimer()
		cp.enqueueFlush(ctx)
		return
	}

	cp.clearTimer()
	delay := cp.config.DelayMs
	if cp.bufferChars >= cp.config.LongThresholdChars {
		delay = cp.config.LongDelayMs
	}

	cp.timer = time.AfterFunc(delay, func() {
		cp.mu.Lock()
		defer cp.mu.Unlock()
		cp.timer = nil
		cp.enqueueFlush(ctx)
	})
}

func (cp *ChatPipeline) Run(ctx context.Context, task func() error) error {
	cp.mu.Lock()
	if len(cp.buffer) > 0 {
		cp.clearTimer()
		cp.enqueueFlush(ctx)
	}
	cp.mu.Unlock()

	errCh := make(chan error, 1)
	cp.tasks <- func() {
		errCh <- task()
	}

	return <-errCh
}

func (cp *ChatPipeline) FlushNow(ctx context.Context) {
	cp.mu.Lock()
	if len(cp.buffer) > 0 {
		cp.clearTimer()
		cp.enqueueFlush(ctx)
	}
	cp.mu.Unlock()

	// Wait for queue to drain
	done := make(chan struct{})
	cp.tasks <- func() {
		close(done)
	}
	<-done
}

func (cp *ChatPipeline) IsIdle() bool {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return len(cp.buffer) == 0 && cp.timer == nil
}

func (cp *ChatPipeline) Dispose() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.closed {
		return
	}
	cp.closed = true
	cp.clearTimer()
	close(cp.stopCh)
	close(cp.tasks)
}

func (cp *ChatPipeline) clearTimer() {
	if cp.timer != nil {
		cp.timer.Stop()
		cp.timer = nil
	}
}

func (cp *ChatPipeline) enqueueFlush(ctx context.Context) {
	if len(cp.buffer) == 0 {
		return
	}

	batch := cp.buffer
	handler := cp.pendingHandler

	cp.buffer = nil
	cp.bufferChars = 0
	cp.pendingHandler = nil

	if handler == nil {
		return
	}

	dispatch := &types.BatchedDispatch{
		Message:   mergeBatch(batch),
		SourceIDs: extractSourceIDs(batch),
	}

	cp.tasks <- func() {
		_ = handler(ctx, dispatch)
	}
}

func mergeBatch(batch []*types.NormalizedMessage) *types.NormalizedMessage {
	if len(batch) == 1 {
		return batch[0]
	}

	last := batch[len(batch)-1]

	var contents []string
	for _, m := range batch {
		if m.Content != "" {
			contents = append(contents, m.Content)
		}
	}
	content := strings.Join(contents, "\n\n")

	var mentionAll, mentionedBot bool
	var resources []types.Resource
	var mentions []types.Mention

	seenResources := make(map[string]bool)
	seenMentions := make(map[string]bool)

	for _, m := range batch {
		if m.MentionAll {
			mentionAll = true
		}
		if m.MentionedBot {
			mentionedBot = true
		}

		for _, r := range m.Resources {
			if !seenResources[r.FileKey] {
				seenResources[r.FileKey] = true
				resources = append(resources, r)
			}
		}

		for _, mn := range m.Mentions {
			id := mn.UserID
			if id == "" {
				id = mn.Name
			}
			if !seenMentions[id] {
				seenMentions[id] = true
				mentions = append(mentions, mn)
			}
		}
	}

	merged := *last
	merged.Content = content
	merged.MentionAll = mentionAll
	merged.MentionedBot = mentionedBot
	merged.Resources = resources
	merged.Mentions = mentions

	return &merged
}

func extractSourceIDs(batch []*types.NormalizedMessage) []string {
	ids := make([]string, len(batch))
	for i, m := range batch {
		ids[i] = m.MessageID
	}
	return ids
}

// ChatPipelineManager manages multiple chat pipelines by scope.
type ChatPipelineManager struct {
	mu        sync.RWMutex
	config    types.BatchConfig
	pipelines map[string]*ChatPipeline
}

func NewChatPipelineManager(config types.BatchConfig) *ChatPipelineManager {
	return &ChatPipelineManager{
		config:    config,
		pipelines: make(map[string]*ChatPipeline),
	}
}

func (cpm *ChatPipelineManager) getOrCreate(scope string, serialOnly bool) *ChatPipeline {
	cpm.mu.RLock()
	p, ok := cpm.pipelines[scope]
	cpm.mu.RUnlock()

	if ok {
		return p
	}

	cpm.mu.Lock()
	defer cpm.mu.Unlock()

	// Double check
	p, ok = cpm.pipelines[scope]
	if ok {
		return p
	}

	p = NewChatPipeline(cpm.config, serialOnly)
	cpm.pipelines[scope] = p
	return p
}

func (cpm *ChatPipelineManager) Push(ctx context.Context, scope string, msg *types.NormalizedMessage, handler FlushHandler) {
	cpm.getOrCreate(scope, false).Push(ctx, msg, handler)
}

func (cpm *ChatPipelineManager) Run(ctx context.Context, scope string, task func() error) error {
	return cpm.getOrCreate(scope, true).Run(ctx, task)
}

func (cpm *ChatPipelineManager) FlushAll(ctx context.Context) {
	cpm.mu.RLock()
	pipelines := make([]*ChatPipeline, 0, len(cpm.pipelines))
	for _, p := range cpm.pipelines {
		pipelines = append(pipelines, p)
	}
	cpm.mu.RUnlock()

	var wg sync.WaitGroup
	for _, p := range pipelines {
		wg.Add(1)
		go func(pipeline *ChatPipeline) {
			defer wg.Done()
			pipeline.FlushNow(ctx)
		}(p)
	}
	wg.Wait()
}

func (cpm *ChatPipelineManager) Dispose() {
	cpm.FlushAll(context.Background())

	cpm.mu.Lock()
	defer cpm.mu.Unlock()

	for _, p := range cpm.pipelines {
		p.Dispose()
	}
	cpm.pipelines = make(map[string]*ChatPipeline)
}
