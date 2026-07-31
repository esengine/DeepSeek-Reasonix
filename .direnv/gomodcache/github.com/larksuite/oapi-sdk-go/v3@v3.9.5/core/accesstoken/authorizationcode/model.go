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

package authorizationcode

type TokenRequest struct {
	Body *TokenRequestBody `body:"body"`
}

type TokenRequestBody struct {
	Code         *string `json:"code,omitempty"`
	RedirectUri  *string `json:"redirect_uri,omitempty"`
	CodeVerifier *string `json:"code_verifier,omitempty"`
	Scope        *string `json:"scope,omitempty"`
}

type TokenRequestBuilder struct {
	body *TokenRequestBody
}

func NewTokenRequestBuilder() *TokenRequestBuilder {
	return &TokenRequestBuilder{body: &TokenRequestBody{}}
}

func (b *TokenRequestBuilder) Code(v string) *TokenRequestBuilder {
	b.body.Code = &v
	return b
}

func (b *TokenRequestBuilder) RedirectUri(v string) *TokenRequestBuilder {
	b.body.RedirectUri = &v
	return b
}

func (b *TokenRequestBuilder) CodeVerifier(v string) *TokenRequestBuilder {
	b.body.CodeVerifier = &v
	return b
}

func (b *TokenRequestBuilder) Scope(v string) *TokenRequestBuilder {
	b.body.Scope = &v
	return b
}

func (b *TokenRequestBuilder) Build() *TokenRequest {
	return &TokenRequest{Body: b.body}
}
