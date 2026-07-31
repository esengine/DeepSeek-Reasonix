// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/labstack/echo/v4"
)

// ContextTimeout Middleware
//
// ContextTimeout provides request timeout functionality using Go's context mechanism.
// It is the recommended replacement for the deprecated Timeout middleware.
//
//
// Basic Usage:
//
//	e.Use(middleware.ContextTimeout(30 * time.Second))
//
// With Configuration:
//
//	e.Use(middleware.ContextTimeoutWithConfig(middleware.ContextTimeoutConfig{
//	    Timeout: 30 * time.Second,
//	    Skipper: middleware.DefaultSkipper,
//	}))
//
// Handler Example:
//
//	e.GET("/task", func(c echo.Context) error {
//	    ctx := c.Request().Context()
//
//	    result, err := performTaskWithContext(ctx)
//	    if err != nil {
//	        if errors.Is(err, context.DeadlineExceeded) {
//	            return echo.NewHTTPError(http.StatusServiceUnavailable, "timeout")
//	        }
//	        return err
//	    }
//
//	    return c.JSON(http.StatusOK, result)
//	})

// ContextTimeoutConfig defines the config for ContextTimeout middleware.
type ContextTimeoutConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// ErrorHandler is a function when error arises in middleware execution.
	ErrorHandler func(err error, c echo.Context) error

	// Timeout configures a timeout for the middleware, defaults to 0 for no timeout
	Timeout time.Duration
}

// ContextTimeout returns a middleware which returns error (503 Service Unavailable error) to client
// when underlying method returns context.DeadlineExceeded error.
func ContextTimeout(timeout time.Duration) echo.MiddlewareFunc {
	return ContextTimeoutWithConfig(ContextTimeoutConfig{Timeout: timeout})
}

// ContextTimeoutWithConfig returns a Timeout middleware with config.
func ContextTimeoutWithConfig(config ContextTimeoutConfig) echo.MiddlewareFunc {
	mw, err := config.ToMiddleware()
	if err != nil {
		panic(err)
	}
	return mw
}

// ToMiddleware converts Config to middleware.
func (config ContextTimeoutConfig) ToMiddleware() (echo.MiddlewareFunc, error) {
	if config.Timeout == 0 {
		return nil, errors.New("timeout must be set")
	}
	if config.Skipper == nil {
		config.Skipper = DefaultSkipper
	}
	if config.ErrorHandler == nil {
		config.ErrorHandler = func(err error, c echo.Context) error {
			if err != nil && errors.Is(err, context.DeadlineExceeded) {
				return echo.ErrServiceUnavailable.WithInternal(err)
			}
			return err
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			timeoutContext, cancel := context.WithTimeout(c.Request().Context(), config.Timeout)
			defer cancel()

			c.SetRequest(c.Request().WithContext(timeoutContext))

			if err := next(c); err != nil {
				return config.ErrorHandler(err, c)
			}
			return nil
		}
	}, nil
}
