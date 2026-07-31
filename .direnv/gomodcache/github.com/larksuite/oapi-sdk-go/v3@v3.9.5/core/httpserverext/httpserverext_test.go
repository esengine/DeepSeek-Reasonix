/*
 * MIT License
 *
 * Copyright (c) 2022 Lark Technologies Pte. Ltd.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice, shall be included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package httpserverext

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/larksuite/oapi-sdk-go/v3/card"
	"github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestStartHttpServer(t *testing.T) {
	// 创建card处理器
	cardHandler := larkcard.NewCardActionHandler("12", "12", func(ctx context.Context, cardAction *larkcard.CardAction) (interface{}, error) {
		fmt.Println(larkcore.Prettify(cardAction))
		return nil, nil
	})

	// 创建事件处理器
	handler := dispatcher.NewEventDispatcher("v", "1212121212").OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		fmt.Println(larkcore.Prettify(event))
		return nil
	}).OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
		fmt.Println(larkcore.Prettify(event))
		return nil
	})

	// 注册事件 和 卡片路径
	http.HandleFunc("/webhook/event", NewEventHandlerFunc(handler))
	http.HandleFunc("/webhook/card", NewCardActionHandlerFunc(cardHandler))

	// 启动服务
	//err := http.ListenAndServe(":9999", nil)
	//if err != nil {
	//	panic(err)
	//}
}

func mockRequest() *http.Request {
	var token = "12"
	value := map[string]interface{}{}
	value["value"] = "sdfsfd"
	value["tag"] = "button"

	cardAction := &larkcard.CardAction{
		OpenID:        "ou_sdfimx9948345",
		UserID:        "eu_sd923r0sdf5",
		OpenMessageID: "om_abcdefg1234567890",
		TenantKey:     "d32004232",
		Token:         token,
		Action: &struct {
			Value      map[string]interface{} `json:"value"`
			Tag        string                 `json:"tag"`
			Option     string                 `json:"option"`
			Timezone   string                 `json:"timezone"`
			Name       string                 `json:"name"`
			FormValue  map[string]interface{} `json:"form_value"`
			InputValue string                 `json:"input_value"`
			Options    []string               `json:"options"`
			Checked    bool                   `json:"checked"`
		}{
			Value: value,
			Tag:   "button",
		},
	}

	cardActionBody := &larkcard.CardActionBody{
		CardAction: cardAction,
		Challenge:  "121212",
		Type:       "url_verification",
	}

	body, _ := json.Marshal(cardActionBody)
	req, _ := http.NewRequest(http.MethodPost, "", bytes.NewBuffer(body))
	req.Header.Set("key1", "value1")
	req.Header.Set("key2", "value2")
	return req
}

func TestTranslate(t *testing.T) {
	req := mockRequest()
	eventReq, err := translate(context.Background(), req)
	if err != nil {
		t.Errorf("translate failed ,%v", err)
		return
	}

	fmt.Println(larkcore.Prettify(eventReq.Header))
	fmt.Println(string(eventReq.Body))
}
