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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/core/accesstoken/authorizationcode"
	"github.com/larksuite/oapi-sdk-go/v3/core/accesstoken/refreshtoken"
)

type AccessToken struct {
	config *larkcore.Config
}

func NewAccessToken(config *larkcore.Config) *AccessToken {
	return &AccessToken{config: config}
}

type accessTokenRequestBody struct {
	GrantType           string `json:"grant_type"`
	ClientAssertionType string `json:"client_assertion_type,omitempty"`
	ClientAssertion     string `json:"client_assertion,omitempty"`
	ClientID            string `json:"client_id"`
	ClientSecret        string `json:"client_secret,omitempty"`
	Code                string `json:"code,omitempty"`
	RedirectUri         string `json:"redirect_uri,omitempty"`
	CodeVerifier        string `json:"code_verifier,omitempty"`
	Scope               string `json:"scope,omitempty"`
	RefreshToken        string `json:"refresh_token,omitempty"`
}

type accessTokenResponseBody struct {
	Code                  int    `json:"code,omitempty"`
	Error                 string `json:"error,omitempty"`
	ErrorDescription      string `json:"error_description,omitempty"`
	AccessToken           string `json:"access_token,omitempty"`
	TokenType             string `json:"token_type,omitempty"`
	ExpiresIn             int    `json:"expires_in,omitempty"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in,omitempty"`
	Scope                 string `json:"scope,omitempty"`
}

func (o *AccessToken) RetrieveByAuthorizationCode(ctx context.Context, req *authorizationcode.TokenRequest, options ...larkcore.RequestOptionFunc) (*AccessTokenResp, error) {
	body := &accessTokenRequestBody{GrantType: larkcore.GrantTypeAuthorizationCode}
	if req != nil && req.Body != nil {
		body.Code = larkcore.StringValue(req.Body.Code)
		body.RedirectUri = larkcore.StringValue(req.Body.RedirectUri)
		body.CodeVerifier = larkcore.StringValue(req.Body.CodeVerifier)
		body.Scope = larkcore.StringValue(req.Body.Scope)
	}
	return o.doAccessTokenRequest(ctx, body, options...)
}

func (o *AccessToken) Refresh(ctx context.Context, req *refreshtoken.TokenRequest, options ...larkcore.RequestOptionFunc) (*AccessTokenResp, error) {
	body := &accessTokenRequestBody{GrantType: larkcore.GrantTypeRefreshToken}
	if req != nil && req.Body != nil {
		body.RefreshToken = larkcore.StringValue(req.Body.RefreshToken)
		body.Scope = larkcore.StringValue(req.Body.Scope)
	}
	return o.doAccessTokenRequest(ctx, body, options...)
}

func (o *AccessToken) doAccessTokenRequest(ctx context.Context, body *accessTokenRequestBody, options ...larkcore.RequestOptionFunc) (*AccessTokenResp, error) {
	oauthBaseUrl, err := larkcore.ResolveOAuthBaseUrl(o.config)
	if err != nil {
		o.config.Logger.Warn(ctx, fmt.Sprintf("resolve oauth base url failed, err:%v", err))
		return nil, err
	}

	body.ClientID = o.config.AppId
	requestURL := strings.TrimRight(oauthBaseUrl, "/") + larkcore.OAuthTokenUrlPath

	if o.config.ClientAssertionProvider != nil {
		aud, err := larkcore.ResolveOAuthAud(o.config)
		if err != nil {
			o.config.Logger.Warn(ctx, fmt.Sprintf("resolve oauth aud failed, err:%v", err))
			return nil, err
		}
		clientAssertionToken, err := o.config.ClientAssertionProvider.RetrieveToken(ctx, aud)
		if err != nil {
			o.config.Logger.Warn(ctx, fmt.Sprintf("retrieve client assertion token failed, aud:%s, err:%v", aud, err))
			return nil, &larkcore.CodeError{Code: larkcore.ErrCodeClientAssertionRetrieveFailed, Msg: err.Error()}
		}
		if clientAssertionToken == nil || clientAssertionToken.Value == "" {
			o.config.Logger.Warn(ctx, fmt.Sprintf("client assertion token is empty, aud:%s", aud))
			return nil, &larkcore.CodeError{Code: larkcore.ErrCodeClientAssertionTokenEmpty, Msg: "client assertion token is empty"}
		}
		body.ClientAssertionType = larkcore.ClientAssertionTypeJWTBearer
		body.ClientAssertion = clientAssertionToken.Value
		if clientAssertionToken.TargetInfo != nil {
			requestURL = buildProxyURL(clientAssertionToken.TargetInfo.TargetService, clientAssertionToken.TargetInfo.TargetPrefix, larkcore.OAuthTokenUrlPath)
			options = append(options, withTargetServiceHeader(aud))
		}
	} else if o.config.AppSecret != "" {
		body.ClientSecret = o.config.AppSecret
	} else {
		return nil, &larkcore.CodeError{Code: larkcore.ErrCodeAppSecretAndClientAssertionEmpty, Msg: "AppSecret and ClientAssertionProvider cannot both be empty for AccessToken APIs"}
	}

	rawResp, err := larkcore.Request(ctx, &larkcore.ApiReq{
		HttpMethod: http.MethodPost,
		ApiPath:    requestURL,
		Body:       body,
		// OAuth token exchange does not require an app, tenant, or user access token.
		SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeNone},
	}, o.config, options...)
	if err != nil {
		return nil, err
	}

	respBody := &accessTokenResponseBody{}
	if err = json.Unmarshal(rawResp.RawBody, respBody); err != nil {
		return nil, err
	}

	if rawResp.StatusCode != http.StatusOK {
		return nil, &AccessTokenError{
			ApiResp:          rawResp,
			Code:             respBody.Code,
			ErrorType:        respBody.Error,
			ErrorDescription: accessTokenErrorDescription(respBody, ""),
		}
	}
	if respBody.Code != 0 || respBody.Error != "" {
		return nil, &AccessTokenError{
			ApiResp:          rawResp,
			Code:             respBody.Code,
			ErrorType:        respBody.Error,
			ErrorDescription: accessTokenErrorDescription(respBody, ""),
		}
	}
	if respBody.AccessToken == "" {
		return nil, &AccessTokenError{
			ApiResp:          rawResp,
			Code:             respBody.Code,
			ErrorType:        respBody.Error,
			ErrorDescription: "access_token is empty",
		}
	}

	return &AccessTokenResp{
		ApiResp: rawResp,
		Data: &AccessTokenRespData{
			AccessToken:           larkcore.StringPtrIfNotEmpty(respBody.AccessToken),
			TokenType:             larkcore.StringPtrIfNotEmpty(respBody.TokenType),
			ExpiresIn:             larkcore.IntPtrIfNotZero(respBody.ExpiresIn),
			RefreshToken:          larkcore.StringPtrIfNotEmpty(respBody.RefreshToken),
			RefreshTokenExpiresIn: larkcore.IntPtrIfNotZero(respBody.RefreshTokenExpiresIn),
			Scope:                 larkcore.StringPtrIfNotEmpty(respBody.Scope),
		},
	}, nil
}

func accessTokenErrorDescription(respBody *accessTokenResponseBody, fallback string) string {
	if respBody == nil {
		return fallback
	}
	if respBody.ErrorDescription != "" {
		return respBody.ErrorDescription
	}
	if respBody.Error != "" {
		return respBody.Error
	}
	return fallback
}

func withTargetServiceHeader(targetService string) larkcore.RequestOptionFunc {
	return func(option *larkcore.RequestOption) {
		if option.Header == nil {
			option.Header = make(http.Header)
		}
		option.Header.Set(larkcore.HeaderXTargetService, targetService)
	}
}

func buildProxyURL(targetService, targetPrefix, apiPath string) string {
	if !strings.Contains(targetService, "://") {
		targetService = "https://" + targetService
	}
	return targetService + targetPrefix + apiPath
}
