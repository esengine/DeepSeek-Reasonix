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

package accesstoken

import (
	"fmt"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type AccessTokenError struct {
	*larkcore.ApiResp `json:"-"`
	Code              int `json:"code,omitempty"`
	// ErrorType is the OAuth error string from the "error" response field, for example "invalid_grant".
	ErrorType        string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (e *AccessTokenError) Error() string {
	msg := e.ErrorDescription
	if msg == "" {
		msg = e.ErrorType
	}
	if msg == "" {
		msg = "access token request failed"
	}
	if e.ApiResp != nil {
		return fmt.Sprintf("statusCode:%d, code:%d, msg:%s", e.ApiResp.StatusCode, e.Code, msg)
	}
	return fmt.Sprintf("code:%d, msg:%s", e.Code, msg)
}
