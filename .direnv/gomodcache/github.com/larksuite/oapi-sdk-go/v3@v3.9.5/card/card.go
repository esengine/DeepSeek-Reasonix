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

package larkcard

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
)

type CardActionHandler struct {
	verificationToken string
	eventEncryptKey   string
	handler           func(context.Context, *CardAction) (interface{}, error)
	*larkcore.Config
}

func processError(ctx context.Context, logger larkcore.Logger, path string, err error) *larkevent.EventResp {
	header := map[string][]string{}
	statusCode := http.StatusInternalServerError
	header[larkevent.ContentTypeHeader] = []string{larkevent.DefaultContentType}
	eventResp := &larkevent.EventResp{
		Header:     header,
		Body:       []byte(fmt.Sprintf(larkevent.WebhookResponseFormat, err.Error())),
		StatusCode: statusCode,
	}
	logger.Error(ctx, fmt.Sprintf("handle cardAcion,path:%s, err: %v", path, err))
	return eventResp
}

func recoveryResult() *larkevent.EventResp {
	header := map[string][]string{}
	statusCode := http.StatusInternalServerError
	header[larkevent.ContentTypeHeader] = []string{larkevent.DefaultContentType}
	eventResp := &larkevent.EventResp{
		Header:     header,
		Body:       []byte(fmt.Sprintf(larkevent.WebhookResponseFormat, "Server Internal Error")),
		StatusCode: statusCode,
	}
	return eventResp
}

func (h *CardActionHandler) Handle(ctx context.Context, req *larkevent.EventReq) (eventResp *larkevent.EventResp) {
	h.Config.Logger.Debug(ctx, fmt.Sprintf("card request: header:%v,body:%s", req.Header, string(req.Body)))
	defer func() {
		if err := recover(); err != nil {
			h.Config.Logger.Error(ctx, fmt.Sprintf("handle cardAction,path:%s, error:%v", req.RequestURI, err))
			eventResp = recoveryResult()
		}
	}()

	plain, err := h.decrypt(req.Body)
	if err != nil {
		return processError(ctx, h.Config.Logger, req.RequestURI, err)
	}

	cardAction := &CardAction{}
	err = json.Unmarshal(plain, cardAction)
	if err != nil {
		return processError(ctx, h.Config.Logger, req.RequestURI, err)
	}
	cardAction.EventReq = req

	if larkevent.ReqType(cardAction.Type) != larkevent.ReqTypeChallenge && !h.Config.SkipSignVerify {
		err = h.VerifySign(ctx, req)
		if err != nil {
			return processError(ctx, h.Config.Logger, req.RequestURI, err)
		}
	}

	result, err := h.DoHandle(ctx, cardAction)
	if err != nil {
		return processError(ctx, h.Config.Logger, req.RequestURI, err)
	}
	return result
}

func (h *CardActionHandler) decrypt(bs []byte) ([]byte, error) {
	var plain []byte
	var encrypt larkevent.EventEncryptMsg
	err := json.Unmarshal(bs, &encrypt)
	if err != nil {
		return nil, err
	}
	if encrypt.Encrypt != "" {
		if h.eventEncryptKey == "" {
			return nil, fmt.Errorf("encrypt_key not found")
		}
		plain, err = larkevent.EventDecrypt(encrypt.Encrypt, h.eventEncryptKey)
		if err != nil {
			return nil, err
		}
	} else {
		plain = bs
	}

	return plain, nil
}

func (h *CardActionHandler) Logger() larkcore.Logger {
	return h.Config.Logger
}

func (h *CardActionHandler) InitConfig(options ...larkevent.OptionFunc) {
	for _, option := range options {
		option(h.Config)
	}
	larkcore.NewLogger(h.Config)
}

func NewCardActionHandler(verificationToken, eventEncryptKey string, handler func(context.Context, *CardAction) (interface{}, error)) *CardActionHandler {
	h := &CardActionHandler{
		verificationToken: verificationToken,
		eventEncryptKey:   eventEncryptKey,
		handler:           handler,
		Config:            &larkcore.Config{Logger: larkcore.NewEventLogger()},
	}
	return h
}

func (h *CardActionHandler) Event() interface{} {
	return &CardAction{}
}

var notFoundCardHandlerErr = errors.New("card action handler not found")

func (h *CardActionHandler) AuthByChallenge(ctx context.Context, cardAction *CardAction) (*larkevent.EventResp, error) {
	header := map[string][]string{}
	header[larkevent.ContentTypeHeader] = []string{larkevent.DefaultContentType}
	hookType := larkevent.ReqType(cardAction.Type)
	challenge := cardAction.Challenge
	if hookType == larkevent.ReqTypeChallenge {
		if h.verificationToken != cardAction.Token {
			err := errors.New("the result of auth by challenge failed")
			return nil, err
		}
		eventResp := larkevent.EventResp{
			Header:     header,
			Body:       []byte(fmt.Sprintf(larkevent.ChallengeResponseFormat, challenge)),
			StatusCode: http.StatusOK,
		}
		h.Config.Logger.Info(ctx, fmt.Sprintf("AuthByChallenge Success"))
		return &eventResp, nil
	}
	return nil, nil
}
func (h *CardActionHandler) DoHandle(ctx context.Context, cardAction *CardAction) (*larkevent.EventResp, error) {
	var err error
	// auth by challenge
	resp, err := h.AuthByChallenge(ctx, cardAction)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		return resp, nil
	}

	// 校验行为执行器
	handler := h.handler
	if handler == nil {
		err = notFoundCardHandlerErr
		return nil, err
	}

	// 执行事件行为处理器
	result, err := handler(ctx, cardAction)
	if err != nil {
		return nil, err
	}

	header := map[string][]string{}
	header[larkevent.ContentTypeHeader] = []string{larkevent.DefaultContentType}
	if result == nil {
		eventResp := &larkevent.EventResp{
			Header:     header,
			Body:       []byte(fmt.Sprintf(larkevent.WebhookResponseFormat, "success")),
			StatusCode: http.StatusOK,
		}
		return eventResp, nil
	}

	var respBody []byte
	switch r := result.(type) {
	case string:
		respBody = []byte(r)
	case *CustomResp:
		statusCode := r.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		b, err := json.Marshal(r.Body)
		if err != nil {
			return nil, err
		}

		eventResp := &larkevent.EventResp{
			Header:     header,
			Body:       b,
			StatusCode: statusCode,
		}
		return eventResp, nil
	default:
		respBody, err = json.Marshal(result)
	}

	eventResp := &larkevent.EventResp{
		Header:     header,
		Body:       respBody,
		StatusCode: http.StatusOK,
	}

	return eventResp, err
}

func (h *CardActionHandler) VerifySign(ctx context.Context, req *larkevent.EventReq) error {
	if h.verificationToken == "" {
		return nil
	}

	// 解析签名头
	requestTimestamps := req.Header[larkevent.EventRequestTimestamp]
	requestNonces := req.Header[larkevent.EventRequestNonce]

	var requestTimestamp = ""
	var requestNonce = ""
	if len(requestTimestamps) > 0 {
		requestTimestamp = requestTimestamps[0]
	}
	if len(requestNonces) > 0 {
		requestNonce = requestNonces[0]
	}

	// 执行sha1签名计算
	targetSign := Signature(requestTimestamp, requestNonce,
		h.verificationToken,
		string(req.Body))

	sourceSigns := req.Header[larkevent.EventSignature]
	var sourceSign = ""
	if len(sourceSigns) > 0 {
		sourceSign = sourceSigns[0]
	}

	// 验签
	if targetSign == sourceSign {
		return nil
	}
	return errors.New("the result of signature verification failed")
}

func Signature(timestamp, nonce, token, body string) string {
	var b strings.Builder
	b.WriteString(timestamp)
	b.WriteString(nonce)
	b.WriteString(token)
	b.WriteString(body)
	bs := []byte(b.String())
	h := sha1.New()
	_, _ = h.Write(bs)
	bs = h.Sum(nil)
	return fmt.Sprintf("%x", bs)
}
