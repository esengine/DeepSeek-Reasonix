package channel

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// triggerReaction 构造一个假的飞书 HTTP Event JSON 载荷，并直接丢给 SDK 的事件分发器处理
func triggerReaction(d *dispatcher.EventDispatcher, eventID, messageID, emojiType string, createTimeMs int64) {
	payload := fmt.Sprintf(`{
		"schema": "2.0",
		"header": {
			"event_id": "%s",
			"event_type": "im.message.reaction.created_v1",
			"create_time": "%d"
		},
		"event": {
			"message_id": "%s",
			"reaction_type": {
				"emoji_type": "%s"
			},
			"operator_type": "user",
			"user_id": {
				"open_id": "ou_123"
			}
		}
	}`, eventID, createTimeMs, messageID, emojiType)

	req := &larkevent.EventReq{
		Header: http.Header{},
		Body:   []byte(payload),
	}
	_ = d.Handle(context.Background(), req)
}

func TestReactionConcurrencyAndDedup(t *testing.T) {
	// 1. 初始化核心组件
	d := dispatcher.NewEventDispatcher("", "")
	wsCli := larkws.NewClient("appID", "appSecret", larkws.WithEventHandler(d))
	cli := lark.NewClient("appID", "appSecret")
	ch := NewChannel(cli, wsCli)

	// 用于测试去重机制
	var dedupCounter int
	var businessDedupCounter int

	// 用于测试串行机制（非并发安全的计数器）
	var unsafeCounter int

	// 2. 注册表情回复回调
	ch.OnReaction(func(ctx context.Context, event *types.ReactionEvent) error {
		if event.ReactionType == "DEDUP_TEST" {
			dedupCounter++
			return nil
		}
		if event.ReactionType == "BIZ_DEDUP_TEST" {
			businessDedupCounter++
			return nil
		}

		// 模拟耗时的非并发安全操作：
		// 如果底层的 Pipeline 串行机制失效，多个协程同时执行到这里，
		// 读取到的 current 可能是同一个旧值，导致最终的 unsafeCounter 小于总数。
		current := unsafeCounter
		time.Sleep(5 * time.Millisecond) // 放大并发冲突概率
		unsafeCounter = current + 1

		return nil
	})

	t.Run("Test Dedup (去重拦截)", func(t *testing.T) {
		var wg sync.WaitGroup
		createTimeMs := time.Now().UnixMilli()
		// 瞬间并发发送 10 个具有 **相同 EventID** 的请求
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				triggerReaction(d, "identical_event_id_123", "msg_1", "DEDUP_TEST", createTimeMs)
			}()
		}
		wg.Wait()
		time.Sleep(50 * time.Millisecond)

		// 期望：只有 1 个请求被执行，其他 9 个被 DedupCache 和 ProcessLock 拦截
		if dedupCounter != 1 {
			t.Errorf("期望去重后执行 1 次，实际执行了 %d 次", dedupCounter)
		}
	})

	t.Run("Test Serialization (串行排队)", func(t *testing.T) {
		var wg sync.WaitGroup
		concurrencyLevel := 50

		// 瞬间并发发送 50 个具有 **不同 EventID**、**相同 MessageID**、但 **不同时间戳** 的请求
		// 它们应共享同一个 message 级串行队列，但不应被 dedup 当成同一条 reaction 事件。
		for i := 0; i < concurrencyLevel; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				eventID := fmt.Sprintf("unique_event_id_%d", idx)
				triggerReaction(d, eventID, "same_message_id_456", "SMILE", 1700000000000+int64(idx))
			}(i)
		}
		wg.Wait()
		// 50个任务 * 5ms = 250ms，等待足够的时间让队列消化完
		time.Sleep(400 * time.Millisecond)

		// 期望：因为这 50 个请求的 MessageID 相同，它们会被底层的 ChatPipeline 放入同一个 Channel 队列中，
		// 交由单个后台 Worker 串行执行。因此，即使 unsafeCounter 不是线程安全的，它也会精准地加到 50！
		if unsafeCounter != concurrencyLevel {
			t.Errorf("期望串行执行 %d 次，实际 unsafeCounter 为 %d (发生了数据竞争导致修改丢失！)", concurrencyLevel, unsafeCounter)
		}
	})

	t.Run("Test Dedup With Different EventID (业务键去重)", func(t *testing.T) {
		businessDedupCounter = 0
		createTimeMs := time.Now().UnixMilli() + 1000
		triggerReaction(d, "biz_evt_1", "msg_business_1", "BIZ_DEDUP_TEST", createTimeMs)
		triggerReaction(d, "biz_evt_2", "msg_business_1", "BIZ_DEDUP_TEST", createTimeMs)
		time.Sleep(80 * time.Millisecond)

		if businessDedupCounter != 1 {
			t.Errorf("期望相同业务 reaction 仅执行 1 次，实际执行了 %d 次", businessDedupCounter)
		}
	})
}
