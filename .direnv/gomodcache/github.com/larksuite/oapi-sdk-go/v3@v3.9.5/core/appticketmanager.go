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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var appTicketManager = AppTicketManager{cache: cache}

func GetAppTicketManager() *AppTicketManager {
	return &appTicketManager
}

type AppTicketManager struct {
	cache Cache
}

func (m *AppTicketManager) Get(ctx context.Context, config *Config) (string, error) {
	ticket, err := m.cache.Get(ctx, appTicketKey(config.AppId))
	if err != nil {
		return "", err
	}
	if ticket == "" {
		applyAppTicket(ctx, config)
	}
	return ticket, nil
}

func (m *AppTicketManager) Set(ctx context.Context, appId, value string, ttl time.Duration) error {
	return m.cache.Set(ctx, appTicketKey(appId), value, ttl)
}

func appTicketKey(appID string) string {
	return fmt.Sprintf("%s-%s", appTicketKeyPrefix, appID)
}

type ResendAppTicketReq struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type ResendAppTicketResp struct {
	*ApiResp `json:"-"`
	CodeError
}

func (r *ResendAppTicketResp) Success() bool {
	return r.Code == 0
}

func applyAppTicket(ctx context.Context, config *Config) {
	rawResp, err := Request(ctx, &ApiReq{
		HttpMethod: http.MethodPost,
		ApiPath:    ApplyAppTicketPath,
		Body: &ResendAppTicketReq{
			AppID:     config.AppId,
			AppSecret: config.AppSecret,
		},
		SupportedAccessTokenTypes: []AccessTokenType{AccessTokenTypeNone},
	}, config)

	if err != nil {
		config.Logger.Error(ctx, fmt.Sprintf("apply app_ticket, error: %v", err))
		return
	}
	if !strings.Contains(rawResp.Header.Get(contentTypeHeader), contentTypeJson) {
		config.Logger.Error(ctx, fmt.Sprintf("apply app_ticket, response content-type not json, response: %v", rawResp))
		return
	}
	codeError := &CodeError{}
	err = json.Unmarshal(rawResp.RawBody, codeError)
	if err != nil {
		config.Logger.Error(ctx, fmt.Sprintf("apply app_ticket, json unmarshal error: %v", err))
		return
	}
	if codeError.Code != 0 {
		config.Logger.Error(ctx, fmt.Sprintf("apply app_ticket, response error: %+v", codeError))
		return
	}
}
