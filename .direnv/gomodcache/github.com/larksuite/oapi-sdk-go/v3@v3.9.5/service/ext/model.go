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

package larkext

import larkcore "github.com/larksuite/oapi-sdk-go/v3/core"

const (
	FileTypeDoc     = "doc"
	FileTypeSheet   = "sheet"
	FileTypeBitable = "bitable"
)

const (
	GrantTypeAuthorizationCode = "authorization_code"
	GrantTypeRefreshCode       = "refresh_token"
)

type AuthenAccessTokenReqBody struct {
	GrantType string `json:"grant_type,omitempty"`
	Code      string `json:"code,omitempty"`
}

type AuthenAccessTokenReqBodyBuilder struct {
	grantType string `json:"grant_type,omitempty"`
	code      string `json:"code,omitempty"`
}

func NewAuthenAccessTokenReqBodyBuilder() *AuthenAccessTokenReqBodyBuilder {
	return &AuthenAccessTokenReqBodyBuilder{}
}

func (a *AuthenAccessTokenReqBodyBuilder) GrantType(grantType string) *AuthenAccessTokenReqBodyBuilder {
	a.grantType = grantType
	return a
}

func (a *AuthenAccessTokenReqBodyBuilder) Code(code string) *AuthenAccessTokenReqBodyBuilder {
	a.code = code
	return a
}

func (a *AuthenAccessTokenReqBodyBuilder) Build() *AuthenAccessTokenReqBody {
	body := &AuthenAccessTokenReqBody{}
	body.GrantType = a.grantType
	body.Code = a.code
	return body
}

type AuthenAccessTokenReq struct {
	apiReq *larkcore.ApiReq
	Body   *AuthenAccessTokenReqBody `body:""`
}

type AuthenAccessTokenReqBuilder struct {
	apiReq *larkcore.ApiReq
	body   *AuthenAccessTokenReqBody `body:""`
}

func NewAuthenAccessTokenReqBuilder() *AuthenAccessTokenReqBuilder {
	return &AuthenAccessTokenReqBuilder{}
}

func (a *AuthenAccessTokenReqBuilder) Body(body *AuthenAccessTokenReqBody) *AuthenAccessTokenReqBuilder {
	a.body = body
	return a
}

func (a *AuthenAccessTokenReqBuilder) Build() *AuthenAccessTokenReq {
	req := &AuthenAccessTokenReq{
		apiReq: &larkcore.ApiReq{
			PathParams:  larkcore.PathParams{},
			QueryParams: larkcore.QueryParams{},
		},
		Body: a.body,
	}
	req.apiReq.Body = a.body
	return req
}

type AuthenAccessTokenResp struct {
	*larkcore.ApiResp `json:"-"`
	larkcore.CodeError
	Data *AuthenAccessTokenRespBody `json:"data"`
}

func (c *AuthenAccessTokenResp) Success() bool {
	return c.Code == 0
}

type AuthenAccessTokenRespBody struct {
	AccessToken      string `json:"access_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	ExpiresIn        int64  `json:"expires_in,omitempty"`
	Name             string `json:"name,omitempty"`
	EnName           string `json:"en_name,omitempty"`
	AvatarURL        string `json:"avatar_url,omitempty"`
	AvatarThumb      string `json:"avatar_thumb,omitempty"`
	AvatarMiddle     string `json:"avatar_middle,omitempty"`
	AvatarBig        string `json:"avatar_big,omitempty"`
	OpenID           string `json:"open_id,omitempty"`  //
	UnionID          string `json:"union_id,omitempty"` //
	Email            string `json:"email,omitempty"`
	EnterpriseEmail  string `json:"enterprise_email,omitempty"`
	UserID           string `json:"user_id,omitempty"`
	Mobile           string `json:"mobile,omitempty"`
	TenantKey        string `json:"tenant_key,omitempty"`
	RefreshExpiresIn int64  `json:"refresh_expires_in,omitempty"`
	RefreshToken     string `json:"refresh_token,omitempty"`
}

type RefreshAuthenAccessTokenReqBody struct {
	GrantType    string `json:"grant_type,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type RefreshAuthenAccessTokenReqBodyBuilder struct {
	grantType    string `json:"grant_type,omitempty"`
	refreshToken string `json:"refresh_token,omitempty"`
}

func NewRefreshAuthenAccessTokenReqBodyBuilder() *RefreshAuthenAccessTokenReqBodyBuilder {
	return &RefreshAuthenAccessTokenReqBodyBuilder{}
}

func (r *RefreshAuthenAccessTokenReqBodyBuilder) GrantType(grantType string) *RefreshAuthenAccessTokenReqBodyBuilder {
	r.grantType = grantType
	return r
}

func (r *RefreshAuthenAccessTokenReqBodyBuilder) RefreshToken(refreshToken string) *RefreshAuthenAccessTokenReqBodyBuilder {
	r.refreshToken = refreshToken
	return r
}

func (r *RefreshAuthenAccessTokenReqBodyBuilder) Build() *RefreshAuthenAccessTokenReqBody {
	body := &RefreshAuthenAccessTokenReqBody{}
	body.GrantType = r.grantType
	body.RefreshToken = r.refreshToken
	return body
}

type RefreshAuthenAccessTokenReq struct {
	apiReq *larkcore.ApiReq
	Body   *RefreshAuthenAccessTokenReqBody `body:""`
}

type RefreshAuthenAccessTokenReqBuilder struct {
	apiReq *larkcore.ApiReq
	body   *RefreshAuthenAccessTokenReqBody `body:""`
}

func NewRefreshAuthenAccessTokenReqBuilder() *RefreshAuthenAccessTokenReqBuilder {
	return &RefreshAuthenAccessTokenReqBuilder{}
}

func (r *RefreshAuthenAccessTokenReqBuilder) Body(body *RefreshAuthenAccessTokenReqBody) *RefreshAuthenAccessTokenReqBuilder {
	r.body = body
	return r
}

func (r *RefreshAuthenAccessTokenReqBuilder) Build() *RefreshAuthenAccessTokenReq {
	req := &RefreshAuthenAccessTokenReq{
		apiReq: &larkcore.ApiReq{
			PathParams:  larkcore.PathParams{},
			QueryParams: larkcore.QueryParams{},
		},
		Body: r.body,
	}
	req.apiReq.Body = r.body
	return req
}

type RefreshAuthenAccessTokenResp struct {
	*larkcore.ApiResp `json:"-"`
	larkcore.CodeError
	Data *RefreshAuthenAccessTokenRespBody `json:"data"`
}

type RefreshAuthenAccessTokenRespBody struct {
	AccessToken      string `json:"access_token,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	ExpiresIn        int64  `json:"expires_in,omitempty"`
	Name             string `json:"name,omitempty"`
	EnName           string `json:"en_name,omitempty"`
	AvatarURL        string `json:"avatar_url,omitempty"`
	AvatarThumb      string `json:"avatar_thumb,omitempty"`
	AvatarMiddle     string `json:"avatar_middle,omitempty"`
	AvatarBig        string `json:"avatar_big,omitempty"`
	OpenID           string `json:"open_id,omitempty"`  //
	UnionID          string `json:"union_id,omitempty"` //
	Email            string `json:"email,omitempty"`
	UserID           string `json:"user_id,omitempty"`
	Mobile           string `json:"mobile,omitempty"`
	TenantKey        string `json:"tenant_key,omitempty"`
	RefreshExpiresIn int64  `json:"refresh_expires_in,omitempty"`
	RefreshToken     string `json:"refresh_token,omitempty"`
}

func (c *RefreshAuthenAccessTokenResp) Success() bool {
	return c.Code == 0
}

type AuthenUserInfoResp struct {
	*larkcore.ApiResp `json:"-"`
	larkcore.CodeError
	Data *AuthenUserInfoRespBody `json:"data"`
}

func (c *AuthenUserInfoResp) Success() bool {
	return c.Code == 0
}

type AuthenUserInfoRespBody struct {
	Name            string `json:"name,omitempty"`
	EnName          string `json:"en_name,omitempty"`
	AvatarURL       string `json:"avatar_url,omitempty"`
	AvatarThumb     string `json:"avatar_thumb,omitempty"`
	AvatarMiddle    string `json:"avatar_middle,omitempty"`
	AvatarBig       string `json:"avatar_big,omitempty"`
	OpenID          string `json:"open_id,omitempty"`
	UnionID         string `json:"union_id,omitempty"`
	Email           string `json:"email,omitempty"`
	EnterpriseEmail string `json:"enterprise_email,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	Mobile          string `json:"mobile,omitempty"`
	TenantKey       string `json:"tenant_key,omitempty"`
}

type CreateFileReq struct {
	apiReq *larkcore.ApiReq
	Body   *CreateFileReqBody `body:""`
}

type CreateFileResp struct {
	*larkcore.ApiResp `json:"-"`
	larkcore.CodeError
	Data *CreateFileRespData `json:"data"`
}

func (c *CreateFileResp) Success() bool {
	return c.Code == 0
}

type CreateFileRespData struct {
	Url      string `json:"url,omitempty"`
	Token    string `json:"token,omitempty"`
	Revision int64  `json:"revision,omitempty"`
}

type CreateFileReqBody struct {
	Title string `json:"title,omitempty"`
	Type_ string `json:"type,omitempty"`
}

type CreateFileReqBodyBuilder struct {
	title string `json:"title,omitempty"`
	type_ string `json:"type,omitempty"`
}

func NewCreateFileReqBodyBuilder() *CreateFileReqBodyBuilder {
	return &CreateFileReqBodyBuilder{}
}

func (c *CreateFileReqBodyBuilder) Title(title string) *CreateFileReqBodyBuilder {
	c.title = title
	return c
}

func (c *CreateFileReqBodyBuilder) Type(type_ string) *CreateFileReqBodyBuilder {
	c.type_ = type_
	return c
}

func (c *CreateFileReqBodyBuilder) Build() *CreateFileReqBody {
	body := &CreateFileReqBody{}
	body.Type_ = c.type_
	body.Title = c.title
	return body
}

type CreateFileReqBuilder struct {
	apiReq *larkcore.ApiReq
	body   *CreateFileReqBody `body:""`
}

func NewCreateFileReqBuilder() *CreateFileReqBuilder {
	builder := &CreateFileReqBuilder{}
	builder.apiReq = &larkcore.ApiReq{
		PathParams:  larkcore.PathParams{},
		QueryParams: larkcore.QueryParams{},
	}
	return builder
}

func (c *CreateFileReqBuilder) FolderToken(folderToken string) *CreateFileReqBuilder {
	c.apiReq.PathParams.Set("folderToken", folderToken)
	return c
}

func (c *CreateFileReqBuilder) Body(body *CreateFileReqBody) *CreateFileReqBuilder {
	c.body = body
	return c
}

func (c *CreateFileReqBuilder) Build() *CreateFileReq {
	req := &CreateFileReq{}
	req.apiReq = &larkcore.ApiReq{}
	req.apiReq.Body = c.body
	req.apiReq.PathParams = c.apiReq.PathParams
	return req
}
