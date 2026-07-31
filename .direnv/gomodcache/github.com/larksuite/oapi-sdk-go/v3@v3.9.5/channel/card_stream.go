package channel

import (
	"context"
	"fmt"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/outbound"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type UpdateQueue struct {
	ch     chan func()
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewUpdateQueue(ctx context.Context) *UpdateQueue {
	ctx, cancel := context.WithCancel(ctx)
	q := &UpdateQueue{
		ch:     make(chan func(), 100),
		ctx:    ctx,
		cancel: cancel,
	}
	q.wg.Add(1)
	go q.loop()
	return q
}

func (q *UpdateQueue) loop() {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case f := <-q.ch:
			f()
		}
	}
}

func (q *UpdateQueue) Submit(f func()) {
	select {
	case <-q.ctx.Done():
	case q.ch <- f:
	}
}

func (q *UpdateQueue) Stop() {
	q.cancel()
	q.wg.Wait()
}

type CardStreamController struct {
	client    *lark.Client
	config    types.ChannelConfig
	messageID string
	queue     *UpdateQueue
	mu        sync.Mutex
	isClosed  bool
}

func NewCardStreamController(client *lark.Client, config types.ChannelConfig, messageID string) *CardStreamController {
	return &CardStreamController{
		client:    client,
		config:    config,
		messageID: messageID,
		queue:     NewUpdateQueue(context.Background()),
	}
}

func (c *CardStreamController) UpdateCard(ctx context.Context, card string) error {
	c.mu.Lock()
	if c.isClosed {
		c.mu.Unlock()
		return fmt.Errorf("stream is closed")
	}
	c.mu.Unlock()

	errCh := make(chan error, 1)

	c.queue.Submit(func() {
		req := larkim.NewPatchMessageReqBuilder().
			MessageId(c.messageID).
			Body(larkim.NewPatchMessageReqBodyBuilder().
				Content(card).
				Build()).
			Build()

		op := func(attempt int) (interface{}, error) {
			resp, err := c.client.Im.V1.Message.Patch(ctx, req)
			if err != nil {
				return nil, err
			}
			if !resp.Success() {
				return nil, &larkcore.CodeError{Code: resp.Code, Msg: resp.Msg}
			}
			return resp, nil
		}
		_, err := outbound.Retry(ctx, op, &outbound.RetryOptions{
			MaxAttempts: c.config.Outbound.Retry.MaxAttempts,
			BaseDelay:   c.config.Outbound.Retry.BaseDelayMs,
		})
		errCh <- err
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (c *CardStreamController) Append(ctx context.Context, text string) error {
	return fmt.Errorf("Append is not supported for CardStreamController, use UpdateCard")
}

func (c *CardStreamController) Flush(ctx context.Context) error {
	// For card updates, they are executed immediately via the queue
	return nil
}

func (c *CardStreamController) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return nil
	}
	c.isClosed = true
	c.queue.Stop()
	return nil
}
