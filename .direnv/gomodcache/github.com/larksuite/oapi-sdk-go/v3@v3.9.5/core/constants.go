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
	"time"
)

const defaultContentType = contentTypeJson + "; charset=utf-8"
const userAgentHeader = "User-Agent"

const (
	HttpHeaderKeyRequestId = "X-Request-Id"
	httpHeaderRequestId    = "Request-Id"
	HttpHeaderKeyLogId     = "X-Tt-Logid"
	contentTypeHeader      = "Content-Type"
	contentTypeJson        = "application/json"
	customRequestId        = "Oapi-Sdk-Request-Id"
)

type AppType string

const (
	AppTypeSelfBuilt   AppType = "SelfBuilt"
	AppTypeMarketplace AppType = "Marketplace"
)

const (
	AppAccessTokenInternalUrlPath    string = "/open-apis/auth/v3/app_access_token/internal"
	AppAccessTokenUrlPath            string = "/open-apis/auth/v3/app_access_token"
	TenantAccessTokenInternalUrlPath string = "/open-apis/auth/v3/tenant_access_token/internal"
	TenantAccessTokenUrlPath         string = "/open-apis/auth/v3/tenant_access_token"
	OAuthTokenUrlPath                string = "/oauth/v3/token"
	ApplyAppTicketPath               string = "/open-apis/auth/v3/app_ticket/resend"
)

const (
	GrantTypeClientCredentials   string = "client_credentials"
	GrantTypeAuthorizationCode   string = "authorization_code"
	GrantTypeRefreshToken        string = "refresh_token"
	GrantTypeJWTBearer           string = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	ClientAssertionTypeJWTBearer string = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	HeaderXTargetService         string = "X-Target-Service"
)

type AccessTokenType string

const (
	AccessTokenTypeNone   AccessTokenType = "none_access_token"
	AccessTokenTypeApp    AccessTokenType = "app_access_token"
	AccessTokenTypeTenant AccessTokenType = "tenant_access_token"
	AccessTokenTypeUser   AccessTokenType = "user_access_token"
)

const (
	appTicketKeyPrefix         = "app_ticket"
	appAccessTokenKeyPrefix    = "app_access_token"
	tenantAccessTokenKeyPrefix = "tenant_access_token"
)
const expiryDelta = 3 * time.Minute
const (
	errCodeAppTicketInvalid                     = 10012
	errCodeAccessTokenInvalid                   = 99991671
	errCodeAppAccessTokenInvalid                = 99991664
	errCodeTenantAccessTokenInvalid             = 99991663
	ErrCodeClientAssertionProviderNotConfigured = 7100
	ErrCodeClientAssertionTokenEmpty            = 7101
	ErrCodeClientAssertionRetrieveFailed        = 7102
	ErrCodeClientAssertionModeNotSupported      = 7103
	ErrCodeAppSecretAndClientAssertionEmpty     = 7104
)
