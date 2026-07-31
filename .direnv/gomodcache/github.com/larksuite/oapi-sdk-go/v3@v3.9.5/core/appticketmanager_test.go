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

package larkcore

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type MockHttpClient struct {
}

func (client *MockHttpClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		Status:           "200",
		StatusCode:       200,
		Proto:            "",
		ProtoMajor:       0,
		ProtoMinor:       0,
		Header:           nil,
		Body:             nil,
		ContentLength:    0,
		TransferEncoding: nil,
		Close:            false,
		Uncompressed:     false,
		Trailer:          nil,
		Request:          nil,
		TLS:              nil,
	}, nil
}
func mockConfig() *Config {

	config := &Config{
		AppId:            "xxx",
		AppSecret:        "xxx",
		Logger:           newLoggerProxy(LogLevelInfo, NewEventLogger()),
		LogLevel:         LogLevelInfo,
		EnableTokenCache: false,
		HttpClient:       &http.Client{},
		AppType:          AppTypeSelfBuilt,
		BaseUrl:          "https://www.baidu.com",
		Serializable:     &DefaultSerialization{},
	}
	return config
}

func TestAppTicketManagerSetAndGet(t *testing.T) {
	config := mockConfig()
	cache := &localCache{}
	appTicketManager := AppTicketManager{cache: cache}

	err := appTicketManager.Set(context.Background(), config.AppId, "appTicketValue", time.Minute)
	if err != nil {
		t.Errorf("set key failed ,%v", err)
	}

	appTicket, err := appTicketManager.Get(context.Background(), config)
	if err != nil {
		t.Errorf("get key failed ,%v", err)
	}

	fmt.Println(appTicket)
}

//
//func TestAppTicketTimeOutAPiGet(t *testing.T) {
//	config := mockConfig()
//	cache := &localCache{}
//	appTicketManager := AppTicketManager{cache: cache}
//
//	err := appTicketManager.Set(context.Background(), config.AppId, "appTicketValue", time.Second)
//	if err != nil {
//		t.Errorf("set key failed ,%v", err)
//	}
//
//	time.Sleep(time.Second * 2)
//
//	appTicket, err := appTicketManager.Get(context.Background(), config)
//	if err != nil {
//		t.Errorf("get key failed ,%v", err)
//	}
//
//	fmt.Println(appTicket)
//}
