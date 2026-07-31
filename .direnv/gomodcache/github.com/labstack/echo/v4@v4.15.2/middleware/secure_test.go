// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestSecure(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	// Default
	Secure()(h)(c)
	assert.Equal(t, "1; mode=block", rec.Header().Get(echo.HeaderXXSSProtection))
	assert.Equal(t, "nosniff", rec.Header().Get(echo.HeaderXContentTypeOptions))
	assert.Equal(t, "SAMEORIGIN", rec.Header().Get(echo.HeaderXFrameOptions))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderStrictTransportSecurity))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderContentSecurityPolicy))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderReferrerPolicy))

	// Custom
	req.Header.Set(echo.HeaderXForwardedProto, "https")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	SecureWithConfig(SecureConfig{
		XSSProtection:         "",
		ContentTypeNosniff:    "",
		XFrameOptions:         "",
		HSTSMaxAge:            3600,
		ContentSecurityPolicy: "default-src 'self'",
		ReferrerPolicy:        "origin",
	})(h)(c)
	assert.Equal(t, "", rec.Header().Get(echo.HeaderXXSSProtection))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderXContentTypeOptions))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderXFrameOptions))
	assert.Equal(t, "max-age=3600; includeSubdomains", rec.Header().Get(echo.HeaderStrictTransportSecurity))
	assert.Equal(t, "default-src 'self'", rec.Header().Get(echo.HeaderContentSecurityPolicy))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderContentSecurityPolicyReportOnly))
	assert.Equal(t, "origin", rec.Header().Get(echo.HeaderReferrerPolicy))

	// Custom with CSPReportOnly flag
	req.Header.Set(echo.HeaderXForwardedProto, "https")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	SecureWithConfig(SecureConfig{
		XSSProtection:         "",
		ContentTypeNosniff:    "",
		XFrameOptions:         "",
		HSTSMaxAge:            3600,
		ContentSecurityPolicy: "default-src 'self'",
		CSPReportOnly:         true,
		ReferrerPolicy:        "origin",
	})(h)(c)
	assert.Equal(t, "", rec.Header().Get(echo.HeaderXXSSProtection))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderXContentTypeOptions))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderXFrameOptions))
	assert.Equal(t, "max-age=3600; includeSubdomains", rec.Header().Get(echo.HeaderStrictTransportSecurity))
	assert.Equal(t, "default-src 'self'", rec.Header().Get(echo.HeaderContentSecurityPolicyReportOnly))
	assert.Equal(t, "", rec.Header().Get(echo.HeaderContentSecurityPolicy))
	assert.Equal(t, "origin", rec.Header().Get(echo.HeaderReferrerPolicy))

	// Custom, with preload option enabled
	req.Header.Set(echo.HeaderXForwardedProto, "https")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	SecureWithConfig(SecureConfig{
		HSTSMaxAge:         3600,
		HSTSPreloadEnabled: true,
	})(h)(c)
	assert.Equal(t, "max-age=3600; includeSubdomains; preload", rec.Header().Get(echo.HeaderStrictTransportSecurity))

	// Custom, with preload option enabled and subdomains excluded
	req.Header.Set(echo.HeaderXForwardedProto, "https")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	SecureWithConfig(SecureConfig{
		HSTSMaxAge:            3600,
		HSTSPreloadEnabled:    true,
		HSTSExcludeSubdomains: true,
	})(h)(c)
	assert.Equal(t, "max-age=3600; preload", rec.Header().Get(echo.HeaderStrictTransportSecurity))
}
