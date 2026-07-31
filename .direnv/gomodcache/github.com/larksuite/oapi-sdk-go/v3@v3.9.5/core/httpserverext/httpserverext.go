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
	"context"
	"fmt"
	"io/ioutil"
	"net/http"

	larkcard "github.com/larksuite/oapi-sdk-go/v3/card"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
)

func doProcess(writer http.ResponseWriter, req *http.Request, reqHandler larkevent.IReqHandler) {
	// 转换http请求对象为标准请求对象
	ctx := context.Background()
	eventReq, err := translate(ctx, req)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		writer.Write([]byte(err.Error()))
		return
	}

	// 处理请求
	eventResp := reqHandler.Handle(ctx, eventReq)

	// 回写结果
	err = write(ctx, writer, eventResp)
	if err != nil {
		reqHandler.Logger().Error(ctx, fmt.Sprintf("write resp result error:%s", err.Error()))
	}
}

func NewCardActionHandlerFunc(cardActionHandler *larkcard.CardActionHandler, options ...larkevent.OptionFunc) func(writer http.ResponseWriter, req *http.Request) {
	cardActionHandler.InitConfig(options...)
	return func(writer http.ResponseWriter, req *http.Request) {
		doProcess(writer, req, cardActionHandler)
	}
}

func NewEventHandlerFunc(eventDispatcher *dispatcher.EventDispatcher, options ...larkevent.OptionFunc) func(writer http.ResponseWriter, req *http.Request) {
	eventDispatcher.InitConfig(options...)
	return func(writer http.ResponseWriter, req *http.Request) {
		doProcess(writer, req, eventDispatcher)
	}
}

func write(ctx context.Context, writer http.ResponseWriter, eventResp *larkevent.EventResp) error {
	writer.WriteHeader(eventResp.StatusCode)
	for k, vs := range eventResp.Header {
		for _, v := range vs {
			writer.Header().Add(k, v)
		}
	}

	if len(eventResp.Body) > 0 {
		_, err := writer.Write(eventResp.Body)
		return err
	}
	return nil
}

func translate(ctx context.Context, req *http.Request) (*larkevent.EventReq, error) {
	rawBody, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	eventReq := &larkevent.EventReq{
		Header:     req.Header,
		Body:       rawBody,
		RequestURI: req.RequestURI,
	}
	return eventReq, nil
}
