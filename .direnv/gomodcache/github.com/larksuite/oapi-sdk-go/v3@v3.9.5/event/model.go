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

package larkevent

import (
	"net/http"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type EventHeader struct {
	EventID    string `json:"event_id"`    // 事件 ID
	EventType  string `json:"event_type"`  // 事件类型
	AppID      string `json:"app_id"`      // 应用 ID
	TenantKey  string `json:"tenant_key"`  // 租户 Key
	CreateTime string `json:"create_time"` // 事件创建时间戳（单位：毫秒）
	Token      string `json:"token"`       // 事件 Token
}

type EventV1Header struct {
	AppID     string `json:"app_id"`       // 应用 ID
	OpenAppID string `json:"open_chat_id"` // Open App Id
	OpenID    string `json:"open_id"`      // Open Id
	TenantKey string `json:"tenant_key"`   // 租户 Key
	Type      string `json:"type"`         // event_callback-事件推送，url_verification-url地址验证
}

type EventV2Base struct {
	Schema string       `json:"schema"` // 事件模式
	Header *EventHeader `json:"header"` // 事件头
}

func (base *EventV2Base) TenantKey() string {
	if base != nil && base.Header != nil {
		return base.Header.TenantKey
	}
	return ""
}

type EventV2Body struct {
	EventV2Base
	Challenge string      `json:"challenge"`
	Event     interface{} `json:"event"`
	Type      string      `json:"type"`
}

type EventReq struct {
	Header     map[string][]string
	Body       []byte
	RequestURI string
}

func (req *EventReq) RequestId() string {
	logID := req.Header[larkcore.HttpHeaderKeyLogId]
	if len(logID) > 0 {
		return logID[0]
	}
	logID = req.Header[larkcore.HttpHeaderKeyRequestId]
	if len(logID) > 0 {
		return logID[0]
	}
	return ""
}

type EventResp struct {
	Header     http.Header // http请求 header
	Body       []byte      // http请求 body
	StatusCode int         // http请求状态码
}

type EventBase struct {
	Ts    string `json:"ts"`    // 事件发送的时间，一般近似于事件发生的时间。
	UUID  string `json:"uuid"`  // 事件的唯一标识
	Token string `json:"token"` // 即Verification Token
	Type  string `json:"type"`  // event_callback-事件推送，url_verification-url地址验证
}

type EventEncryptMsg struct {
	Encrypt string `json:"encrypt"`
}

type EventFuzzy struct {
	Encrypt   string       `json:"encrypt"`
	Schema    string       `json:"schema"`
	Token     string       `json:"token"`
	Type      string       `json:"type"`
	Challenge string       `json:"challenge"`
	Header    *EventHeader `json:"header"`
	Event     *struct {
		Type interface{} `json:"type"`
	} `json:"event"`
}

const (
	EventRequestNonce     = "X-Lark-Request-Nonce"
	EventRequestTimestamp = "X-Lark-Request-Timestamp"
	EventSignature        = "X-Lark-Signature"
)

type ReqType string

const (
	ReqTypeChallenge     ReqType = "url_verification"
	ReqTypeEventCallBack ReqType = "event_callback"
)

const userAgentHeader = "User-Agent"
const ContentTypeHeader = "Content-Type"
const ContentTypeJson = "application/json"
const DefaultContentType = ContentTypeJson + "; charset=utf-8"
const WebhookResponseFormat = `{"msg":"%s"}`
const ChallengeResponseFormat = `{"challenge":"%s"}`
