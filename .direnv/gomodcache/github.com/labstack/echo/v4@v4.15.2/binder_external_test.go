// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

// run tests as external package to get real feel for API
package echo_test

import (
	"encoding/base64"
	"fmt"
	"github.com/labstack/echo/v4"
	"log"
	"net/http"
	"net/http/httptest"
)

func ExampleValueBinder_BindErrors() {
	// example route function that binds query params to different destinations and returns all bind errors in one go
	routeFunc := func(c echo.Context) error {
		var opts struct {
			Active bool
			IDs    []int64
		}
		length := int64(50) // default length is 50

		b := echo.QueryParamsBinder(c)

		errs := b.Int64("length", &length).
			Int64s("ids", &opts.IDs).
			Bool("active", &opts.Active).
			BindErrors() // returns all errors
		if errs != nil {
			for _, err := range errs {
				bErr := err.(*echo.BindingError)
				log.Printf("in case you want to access what field: %s values: %v failed", bErr.Field, bErr.Values)
			}
			return fmt.Errorf("%v fields failed to bind", len(errs))
		}
		fmt.Printf("active = %v, length = %v, ids = %v", opts.Active, length, opts.IDs)

		return c.JSON(http.StatusOK, opts)
	}

	e := echo.New()
	c := e.NewContext(
		httptest.NewRequest(http.MethodGet, "/api/endpoint?active=true&length=25&ids=1&ids=2&ids=3", nil),
		httptest.NewRecorder(),
	)

	_ = routeFunc(c)

	// Output: active = true, length = 25, ids = [1 2 3]
}

func ExampleValueBinder_BindError() {
	// example route function that binds query params to different destinations and stops binding on first bind error
	failFastRouteFunc := func(c echo.Context) error {
		var opts struct {
			Active bool
			IDs    []int64
		}
		length := int64(50) // default length is 50

		// create binder that stops binding at first error
		b := echo.QueryParamsBinder(c)

		err := b.Int64("length", &length).
			Int64s("ids", &opts.IDs).
			Bool("active", &opts.Active).
			BindError() // returns first binding error
		if err != nil {
			bErr := err.(*echo.BindingError)
			return fmt.Errorf("my own custom error for field: %s values: %v", bErr.Field, bErr.Values)
		}
		fmt.Printf("active = %v, length = %v, ids = %v\n", opts.Active, length, opts.IDs)

		return c.JSON(http.StatusOK, opts)
	}

	e := echo.New()
	c := e.NewContext(
		httptest.NewRequest(http.MethodGet, "/api/endpoint?active=true&length=25&ids=1&ids=2&ids=3", nil),
		httptest.NewRecorder(),
	)

	_ = failFastRouteFunc(c)

	// Output: active = true, length = 25, ids = [1 2 3]
}

func ExampleValueBinder_CustomFunc() {
	// example route function that binds query params using custom function closure
	routeFunc := func(c echo.Context) error {
		length := int64(50) // default length is 50
		var binary []byte

		b := echo.QueryParamsBinder(c)
		errs := b.Int64("length", &length).
			CustomFunc("base64", func(values []string) []error {
				if len(values) == 0 {
					return nil
				}
				decoded, err := base64.URLEncoding.DecodeString(values[0])
				if err != nil {
					// in this example we use only first param value but url could contain multiple params in reality and
					// therefore in theory produce multiple binding errors
					return []error{echo.NewBindingError("base64", values[0:1], "failed to decode base64", err)}
				}
				binary = decoded
				return nil
			}).
			BindErrors() // returns all errors

		if errs != nil {
			for _, err := range errs {
				bErr := err.(*echo.BindingError)
				log.Printf("in case you want to access what field: %s values: %v failed", bErr.Field, bErr.Values)
			}
			return fmt.Errorf("%v fields failed to bind", len(errs))
		}
		fmt.Printf("length = %v, base64 = %s", length, binary)

		return c.JSON(http.StatusOK, "ok")
	}

	e := echo.New()
	c := e.NewContext(
		httptest.NewRequest(http.MethodGet, "/api/endpoint?length=25&base64=SGVsbG8gV29ybGQ%3D", nil),
		httptest.NewRecorder(),
	)
	_ = routeFunc(c)

	// Output: length = 25, base64 = Hello World
}
