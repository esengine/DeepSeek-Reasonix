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

import "net/url"

type ApiReq struct {
	HttpMethod                string
	ApiPath                   string
	Body                      interface{}
	QueryParams               QueryParams
	PathParams                PathParams
	SupportedAccessTokenTypes []AccessTokenType
}

type PathParams map[string]string

func (u PathParams) Get(key string) string {
	vs := u[key]
	if len(vs) == 0 {
		return ""
	}
	return vs
}
func (u PathParams) Set(key, value string) {
	u[key] = value
}

type QueryParams map[string][]string

func (u QueryParams) Get(key string) string {
	vs := u[key]
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}
func (u QueryParams) Set(key, value string) {
	u[key] = []string{value}
}

func (u QueryParams) Encode() string {
	return url.Values(u).Encode()
}

func (u QueryParams) Add(key, value string) {
	u[key] = append(u[key], value)
}
