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
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var reqTranslator ReqTranslator

func NewHttpClient(config *Config) {
	if config.HttpClient == nil {
		if config.ReqTimeout == 0 {
			config.HttpClient = http.DefaultClient
		} else {
			config.HttpClient = &http.Client{Timeout: config.ReqTimeout}
		}
	}
}

func NewSerialization(config *Config) {
	if config.Serializable == nil {
		config.Serializable = &DefaultSerialization{}
	}
}

func validateTokenType(accessTokenTypes []AccessTokenType, option *RequestOption) error {
	if option == nil || len(accessTokenTypes) > 1 {
		return nil
	}

	accessTokenType := accessTokenTypes[0]
	if accessTokenType == AccessTokenTypeTenant && option.UserAccessToken != "" {
		return errors.New("tenant token type not match user access token")
	}
	if accessTokenType == AccessTokenTypeUser && option.TenantAccessToken != "" {
		return errors.New("user token type not match tenant access token")
	}
	return nil
}

func determineTokenType(accessTokenTypes []AccessTokenType, option *RequestOption, config *Config) (AccessTokenType, error) {
	if config.ClientAssertionProvider != nil {
		accessibleTokenTypeSet := make(map[AccessTokenType]struct{})
		accessTokenType := accessTokenTypes[0]
		for _, t := range accessTokenTypes {
			if t == AccessTokenTypeTenant {
				accessTokenType = t
			}
			accessibleTokenTypeSet[t] = struct{}{}
		}

		if option.UserAccessToken != "" {
			if _, ok := accessibleTokenTypeSet[AccessTokenTypeUser]; ok {
				return AccessTokenTypeUser, nil
			}
		}
		if _, ok := accessibleTokenTypeSet[AccessTokenTypeTenant]; ok {
			return AccessTokenTypeTenant, nil
		}
		if _, ok := accessibleTokenTypeSet[AccessTokenTypeApp]; ok {
			return AccessTokenTypeNone, &CodeError{Code: ErrCodeClientAssertionModeNotSupported, Msg: "AppAccessToken APIs are not available in ClientAssertion mode"}
		}
		return accessTokenType, nil
	}

	if !config.EnableTokenCache {
		if option.UserAccessToken != "" {
			return AccessTokenTypeUser, nil
		}
		if option.TenantAccessToken != "" {
			return AccessTokenTypeTenant, nil
		}
		if option.AppAccessToken != "" {
			return AccessTokenTypeApp, nil
		}

		return AccessTokenTypeNone, nil
	}
	accessibleTokenTypeSet := make(map[AccessTokenType]struct{})
	accessTokenType := accessTokenTypes[0]
	for _, t := range accessTokenTypes {
		if t == AccessTokenTypeTenant {
			accessTokenType = t // default
		}
		accessibleTokenTypeSet[t] = struct{}{}
	}
	if option.TenantKey != "" {
		if _, ok := accessibleTokenTypeSet[AccessTokenTypeTenant]; ok {
			accessTokenType = AccessTokenTypeTenant
		}
	}
	if option.UserAccessToken != "" {
		if _, ok := accessibleTokenTypeSet[AccessTokenTypeUser]; ok {
			accessTokenType = AccessTokenTypeUser
		}
	}

	return accessTokenType, nil
}

func validate(config *Config, option *RequestOption, accessTokenType AccessTokenType) error {
	if config.AppId == "" {
		return &IllegalParamError{msg: "AppId is empty"}
	}

	hasManualAccessToken := (accessTokenType == AccessTokenTypeUser && option.UserAccessToken != "") ||
		(accessTokenType == AccessTokenTypeTenant && option.TenantAccessToken != "") ||
		(accessTokenType == AccessTokenTypeApp && option.AppAccessToken != "")

	if config.ClientAssertionProvider != nil && config.AppType == AppTypeMarketplace {
		return &CodeError{Code: ErrCodeClientAssertionProviderNotConfigured, Msg: "ClientAssertion mode is not supported for marketplace apps"}
	}

	if config.ClientAssertionProvider == nil && config.AppSecret == "" && !hasManualAccessToken {
		return &IllegalParamError{msg: "AppSecret is empty"}
	}

	if !config.EnableTokenCache {
		if accessTokenType == AccessTokenTypeNone {
			return nil
		}
		if option.UserAccessToken == "" && option.TenantAccessToken == "" && option.AppAccessToken == "" {
			return &IllegalParamError{msg: "accessToken is empty"}
		}
	}

	if config.AppType == AppTypeMarketplace && accessTokenType == AccessTokenTypeTenant && option.TenantKey == "" {
		return &IllegalParamError{msg: "tenant key is empty"}
	}

	if accessTokenType == AccessTokenTypeUser && option.UserAccessToken == "" {
		return &IllegalParamError{msg: "user access token is empty"}
	}

	if option.Header != nil {
		if option.Header.Get(HttpHeaderKeyRequestId) != "" {
			return &IllegalParamError{msg: fmt.Sprintf("use %s as header key is not allowed", HttpHeaderKeyRequestId)}
		}
		if option.Header.Get(httpHeaderRequestId) != "" {
			return &IllegalParamError{msg: fmt.Sprintf("use %s as header key is not allowed", httpHeaderRequestId)}
		}
	}

	return nil
}

func shouldSkipCodeErrorPreDecode(apiPath string) bool {
	if strings.Contains(apiPath, "://") {
		parsedURL, err := url.Parse(apiPath)
		if err == nil {
			apiPath = parsedURL.Path
		}
	}

	return strings.HasSuffix(apiPath, OAuthTokenUrlPath)
}

func safeRequestForDebug(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	safeReq := req.Clone(req.Context())
	safeReq.Header = req.Header.Clone()
	if safeReq.Header.Get("Authorization") != "" {
		safeReq.Header.Set("Authorization", "<redacted>")
	}
	if safeReq.Header.Get("X-Lark-Helpdesk-Authorization") != "" {
		safeReq.Header.Set("X-Lark-Helpdesk-Authorization", "<redacted>")
	}
	return safeReq
}

func doSend(ctx context.Context, rawRequest *http.Request, httpClient HttpClient, logger Logger) (*ApiResp, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(rawRequest)
	if err != nil {
		if er, ok := err.(*url.Error); ok {
			if er.Timeout() {
				return nil, &ClientTimeoutError{msg: er.Error()}
			}

			if e, ok := er.Err.(*net.OpError); ok && e.Op == "dial" {
				return nil, &DialFailedError{msg: er.Error()}
			}
		}
		return nil, err
	}

	if resp.StatusCode == http.StatusGatewayTimeout {
		logID := resp.Header.Get(HttpHeaderKeyLogId)
		if logID == "" {
			logID = resp.Header.Get(HttpHeaderKeyRequestId)
		}
		logger.Info(ctx, fmt.Sprintf("req path:%s, server time out,requestId:%s",
			rawRequest.URL.RequestURI(), logID))
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, &ServerTimeoutError{msg: "server time out error"}
	}
	body, err := readResponse(resp)
	if err != nil {
		return nil, err
	}

	return &ApiResp{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		RawBody:    body,
	}, nil
}

func Request(ctx context.Context, req *ApiReq, config *Config, options ...RequestOptionFunc) (*ApiResp, error) {
	option := &RequestOption{}
	for _, optionFunc := range options {
		optionFunc(option)
	}

	// 兼容 auth_v3
	if len(req.SupportedAccessTokenTypes) == 0 {
		req.SupportedAccessTokenTypes = append(req.SupportedAccessTokenTypes, AccessTokenTypeNone)
	}

	err := validateTokenType(req.SupportedAccessTokenTypes, option)
	if err != nil {
		return nil, err
	}
	accessTokenType, err := determineTokenType(req.SupportedAccessTokenTypes, option, config)
	if err != nil {
		return nil, err
	}
	err = validate(config, option, accessTokenType)
	if err != nil {
		return nil, err
	}

	return doRequest(ctx, req, accessTokenType, config, option)

}

func doRequest(ctx context.Context, httpReq *ApiReq, accessTokenType AccessTokenType, config *Config, option *RequestOption) (*ApiResp, error) {
	var rawResp *ApiResp
	var errResult error
	for i := 0; i < 2; i++ {
		req, err := reqTranslator.translate(ctx, httpReq, accessTokenType, config, option)
		if err != nil {
			config.Logger.Warn(ctx, fmt.Sprintf("translate request failed, err:%v", err))
			return nil, err
		}

		if config.LogReqAtDebug {
			if shouldSkipCodeErrorPreDecode(httpReq.ApiPath) {
				config.Logger.Debug(ctx, fmt.Sprintf("req:%s,%s, oauth token request body omitted", httpReq.HttpMethod, httpReq.ApiPath))
			} else {
				config.Logger.Debug(ctx, fmt.Sprintf("req:%v", safeRequestForDebug(req)))
			}
		} else {
			config.Logger.Debug(ctx, fmt.Sprintf("req:%s,%s", httpReq.HttpMethod, httpReq.ApiPath))
		}
		rawResp, err = doSend(ctx, req, config.HttpClient, config.Logger)
		if config.LogReqAtDebug {
			if shouldSkipCodeErrorPreDecode(httpReq.ApiPath) {
				statusCode := 0
				if rawResp != nil {
					statusCode = rawResp.StatusCode
				}
				config.Logger.Debug(ctx, fmt.Sprintf("resp:statusCode:%d, oauth token response body omitted", statusCode))
			} else {
				config.Logger.Debug(ctx, fmt.Sprintf("resp:%v", rawResp))
			}
		}
		_, isDialError := err.(*DialFailedError)
		if err != nil && !isDialError {
			return nil, err
		}
		errResult = err
		if isDialError {
			continue
		}

		fileDownloadSuccess := option.FileDownload && rawResp.StatusCode == http.StatusOK
		if fileDownloadSuccess || !strings.Contains(rawResp.Header.Get(contentTypeHeader), contentTypeJson) {
			break
		}
		if shouldSkipCodeErrorPreDecode(httpReq.ApiPath) {
			break
		}

		codeError := &CodeError{}
		err = config.Serializable.Deserialize(rawResp.RawBody, codeError)
		if err != nil {
			return nil, err
		}

		code := codeError.Code
		if code == errCodeAppTicketInvalid {
			applyAppTicket(ctx, config)
		}

		if accessTokenType == AccessTokenTypeNone {
			break
		}

		if !config.EnableTokenCache {
			break
		}

		if code != errCodeAccessTokenInvalid && code != errCodeAppAccessTokenInvalid &&
			code != errCodeTenantAccessTokenInvalid {
			break
		}
	}

	if errResult != nil {
		return nil, errResult
	}
	return rawResp, nil
}
