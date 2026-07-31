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
	"net/http"
	"time"
)

type TargetInfo struct {
	TargetService string
	TargetPrefix  string
}

type Token struct {
	Value      string
	TargetInfo *TargetInfo
}

type ClientAssertionProvider interface {
	RetrieveToken(ctx context.Context, aud string) (*Token, error)
}

type Config struct {
	BaseUrl                 string
	OAuthBaseUrl            string
	AppId                   string
	AppSecret               string
	ClientAssertionProvider ClientAssertionProvider
	HelpDeskId              string
	HelpDeskToken           string
	HelpdeskAuthToken       string
	ReqTimeout              time.Duration
	LogLevel                LogLevel
	HttpClient              HttpClient
	Logger                  Logger
	AppType                 AppType
	EnableTokenCache        bool
	TokenCache              Cache
	LogReqAtDebug           bool
	Header                  http.Header
	Serializable            Serializable
	SkipSignVerify          bool
	Source                  string
}
