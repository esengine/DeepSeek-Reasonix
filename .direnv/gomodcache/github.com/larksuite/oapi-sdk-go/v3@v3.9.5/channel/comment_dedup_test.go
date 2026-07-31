package channel

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func triggerComment(d *dispatcher.EventDispatcher, eventID, commentID, fileToken string, createTimeMs int64) {
	payload := fmt.Sprintf(`{
		"schema": "2.0",
		"header": {
			"event_id": "%s",
			"event_type": "drive.notice.comment_add_v1",
			"create_time": "%d"
		},
		"event": {
			"comment_id": "%s",
			"file_token": "%s",
			"file_type": "docx",
			"create_time": "%d",
			"user_id": {
				"open_id": "ou_commenter_123"
			}
		}
	}`, eventID, createTimeMs, commentID, fileToken, createTimeMs)

	req := &larkevent.EventReq{
		Header: http.Header{},
		Body:   []byte(payload),
	}
	_ = d.Handle(context.Background(), req)
}

func TestCommentDedupUsesBusinessKey(t *testing.T) {
	d := dispatcher.NewEventDispatcher("", "")
	wsCli := larkws.NewClient("appID", "appSecret", larkws.WithEventHandler(d))
	cli := lark.NewClient("appID", "appSecret")
	ch := NewChannel(cli, wsCli)

	var handled int32
	ch.OnComment(func(ctx context.Context, event *types.CommentEvent) error {
		atomic.AddInt32(&handled, 1)
		return nil
	})

	nowMs := time.Now().UnixMilli()
	triggerComment(d, "evt_comment_1", "comment_same_123", "file_token_1", nowMs)
	time.Sleep(80 * time.Millisecond)

	if got := atomic.LoadInt32(&handled); got != 1 {
		t.Fatalf("expected first comment to be handled once, got %d", got)
	}

	triggerComment(d, "evt_comment_2", "comment_same_123", "file_token_1", nowMs+1)
	time.Sleep(80 * time.Millisecond)

	if got := atomic.LoadInt32(&handled); got != 1 {
		t.Fatalf("expected duplicate comment business key to be dropped, got %d handler calls", got)
	}
}
