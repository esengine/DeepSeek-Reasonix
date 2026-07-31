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
	"net/http"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type AccessTokenResp struct {
	*larkcore.ApiResp `json:"-"`
	Data              *AccessTokenRespData `json:"data,omitempty"`
}

type AccessTokenRespData struct {
	AccessToken           *string `json:"access_token,omitempty"`
	TokenType             *string `json:"token_type,omitempty"`
	ExpiresIn             *int    `json:"expires_in,omitempty"`
	RefreshToken          *string `json:"refresh_token,omitempty"`
	RefreshTokenExpiresIn *int    `json:"refresh_token_expires_in,omitempty"`
	Scope                 *string `json:"scope,omitempty"`
}

func (r *AccessTokenResp) Success() bool {
	return r.ApiResp != nil && r.ApiResp.StatusCode == http.StatusOK
}
