package normalize

import (
	"reflect"
	"testing"

	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestParseContent(t *testing.T) {
	tests := []struct {
		name         string
		msgType      string
		content      string
		wantContent  string
		wantResource []types.Resource
	}{
		{
			name:         "text",
			msgType:      "text",
			content:      `{"text":"hello"}`,
			wantContent:  "hello",
			wantResource: nil,
		},
		{
			name:         "image",
			msgType:      "image",
			content:      `{"image_key":"img_123"}`,
			wantContent:  "![image](img_123)",
			wantResource: []types.Resource{{Type: "image", FileKey: "img_123"}},
		},
		{
			name:         "file",
			msgType:      "file",
			content:      `{"file_key":"file_123", "file_name":"test.txt"}`,
			wantContent:  `<file key="file_123" name="test.txt"/>`,
			wantResource: []types.Resource{{Type: "file", FileKey: "file_123", FileName: "test.txt"}},
		},
		{
			name:         "folder",
			msgType:      "folder",
			content:      `{"file_key":"folder_123", "file_name":"my_folder"}`,
			wantContent:  `<folder key="folder_123" name="my_folder"/>`,
			wantResource: nil,
		},
		{
			name:         "audio",
			msgType:      "audio",
			content:      `{"file_key":"audio_123", "duration": 120000}`,
			wantContent:  `<audio key="audio_123" duration="2:00"/>`,
			wantResource: []types.Resource{{Type: "audio", FileKey: "audio_123", DurationMs: intPtr(120000)}},
		},
		{
			name:         "video",
			msgType:      "media",
			content:      `{"file_key":"video_123", "file_name":"vid.mp4", "duration": 65000, "image_key":"cover_1"}`,
			wantContent:  `<video key="video_123" name="vid.mp4" duration="1:05"/>`,
			wantResource: []types.Resource{{Type: "video", FileKey: "video_123", FileName: "vid.mp4", CoverImageKey: "cover_1", DurationMs: intPtr(65000)}},
		},
		{
			name:         "sticker",
			msgType:      "sticker",
			content:      `{"file_key":"sticker_123"}`,
			wantContent:  `<sticker key="sticker_123"/>`,
			wantResource: []types.Resource{{Type: "sticker", FileKey: "sticker_123"}},
		},
		{
			name:         "hongbao",
			msgType:      "hongbao",
			content:      `{"text":"Happy New Year"}`,
			wantContent:  `<hongbao text="Happy New Year"/>`,
			wantResource: nil,
		},
		{
			name:         "location",
			msgType:      "location",
			content:      `{"name":"Beijing", "latitude":"39.9042", "longitude":"116.4074"}`,
			wantContent:  `<location name="Beijing" coords="lat:39.9042,lng:116.4074"/>`,
			wantResource: nil,
		},
		{
			name:         "share_chat",
			msgType:      "share_chat",
			content:      `{"chat_id":"chat_123"}`,
			wantContent:  `<group_card id="chat_123"/>`,
			wantResource: nil,
		},
		{
			name:         "share_user",
			msgType:      "share_user",
			content:      `{"user_id":"user_123"}`,
			wantContent:  `<contact_card id="user_123"/>`,
			wantResource: nil,
		},
		{
			name:         "system",
			msgType:      "system",
			content:      `{"template":"Hello {name}, welcome to {place}", "name":"Alice", "place":"Wonderland"}`,
			wantContent:  "Hello Alice, welcome to Wonderland",
			wantResource: nil,
		},
		{
			name:         "vote",
			msgType:      "vote",
			content:      `{"topic":"What's for lunch?", "options":["Pizza", "Burger"]}`,
			wantContent:  "<vote>\nWhat's for lunch?\n• Pizza\n• Burger\n</vote>",
			wantResource: nil,
		},
		{
			name:         "video_chat",
			msgType:      "video_chat",
			content:      `{"topic":"Daily Sync", "start_time":"1609459200000"}`, // 2021-01-01 08:00:00 UTC
			wantContent:  "<meeting>\n📹 Daily Sync\n🕙 2021-01-01 08:00:00\n</meeting>",
			wantResource: nil,
		},
		{
			name:         "calendar",
			msgType:      "calendar",
			content:      `{"summary":"Meeting", "start_time":"1609459200000", "end_time":"1609462800000"}`,
			wantContent:  "<calendar_invite>\n📅 Meeting\n🕙 2021-01-01 08:00:00 ~ 2021-01-01 09:00:00\n</calendar_invite>",
			wantResource: nil,
		},
		{
			name:         "todo",
			msgType:      "todo",
			content:      `{"summary":{"title":"Buy milk", "content":[[{"tag":"text","text":"From the store"}]]}, "due_time":"1609459200000"}`,
			wantContent:  "<todo>\nBuy milk\nFrom the store\nDue: 2021-01-01 08:00:00\n</todo>",
			wantResource: nil,
		},
		{
			name:         "post",
			msgType:      "post",
			content:      `{"zh_cn": {"title":"Post Title", "content":[[{"tag":"text","text":"Hello "}, {"tag":"a", "text":"Link", "href":"https://example.com"}], [{"tag":"at", "user_id":"all"}], [{"tag":"img", "image_key":"img_abc"}]]}}`,
			wantContent:  "**Post Title**\n\nHello [Link](https://example.com)\n@all\n![image](img_abc)",
			wantResource: []types.Resource{{Type: "image", FileKey: "img_abc"}},
		},
		{
			name:         "interactive",
			msgType:      "interactive",
			content:      `{"config":{"wide_screen_mode":true}}`,
			wantContent:  "[interactive card]",
			wantResource: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotContent, gotResource := ParseContent(tt.msgType, tt.content)
			if gotContent != tt.wantContent {
				t.Errorf("ParseContent() gotContent = %v, want %v", gotContent, tt.wantContent)
			}
			if !reflect.DeepEqual(gotResource, tt.wantResource) {
				t.Errorf("ParseContent() gotResource = %+v, want %+v", gotResource, tt.wantResource)
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}
