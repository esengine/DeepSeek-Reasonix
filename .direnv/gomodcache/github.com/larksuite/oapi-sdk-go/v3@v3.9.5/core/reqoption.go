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

import "net/http"

type RequestOption struct {
	TenantKey         string
	UserAccessToken   string
	AppAccessToken    string
	TenantAccessToken string
	NeedHelpDeskAuth  bool
	RequestId         string
	AppTicket         string
	FileUpload        bool
	FileDownload      bool
	Header            http.Header
}

type RequestOptionFunc func(option *RequestOption)

func WithNeedHelpDeskAuth() RequestOptionFunc {
	return func(option *RequestOption) {
		option.NeedHelpDeskAuth = true
	}
}

func WithRequestId(requestId string) RequestOptionFunc {
	return func(option *RequestOption) {
		option.RequestId = requestId
	}
}

func WithTenantKey(tenantKey string) RequestOptionFunc {
	return func(option *RequestOption) {
		option.TenantKey = tenantKey
	}
}

func WithAppTicket(appTicket string) RequestOptionFunc {
	return func(option *RequestOption) {
		option.AppTicket = appTicket
	}
}

func WithFileUpload() RequestOptionFunc {
	return func(option *RequestOption) {
		option.FileUpload = true
	}
}

func WithFileDownload() RequestOptionFunc {
	return func(option *RequestOption) {
		option.FileDownload = true
	}
}

func WithHeaders(header http.Header) RequestOptionFunc {
	return func(option *RequestOption) {
		option.Header = header
	}
}

func WithUserAccessToken(userAccessToken string) RequestOptionFunc {
	return func(option *RequestOption) {
		option.UserAccessToken = userAccessToken
	}
}

func WithTenantAccessToken(tenantAccessToken string) RequestOptionFunc {
	return func(option *RequestOption) {
		option.TenantAccessToken = tenantAccessToken
	}
}
