// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func createTestContext(URL string, body io.Reader, pathParams map[string]string) Context {
	e := New()
	req := httptest.NewRequest(http.MethodGet, URL, body)
	if body != nil {
		req.Header.Set(HeaderContentType, MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if len(pathParams) > 0 {
		names := make([]string, 0)
		values := make([]string, 0)
		for name, value := range pathParams {
			names = append(names, name)
			values = append(values, value)
		}
		c.SetParamNames(names...)
		c.SetParamValues(values...)
	}

	return c
}

func TestBindingError_Error(t *testing.T) {
	err := NewBindingError("id", []string{"1", "nope"}, "bind failed", errors.New("internal error"))
	assert.EqualError(t, err, `code=400, message=bind failed, internal=internal error, field=id`)

	bErr := err.(*BindingError)
	assert.Equal(t, 400, bErr.Code)
	assert.Equal(t, "bind failed", bErr.Message)
	assert.Equal(t, errors.New("internal error"), bErr.Internal)

	assert.Equal(t, "id", bErr.Field)
	assert.Equal(t, []string{"1", "nope"}, bErr.Values)
}

func TestBindingError_ErrorJSON(t *testing.T) {
	err := NewBindingError("id", []string{"1", "nope"}, "bind failed", errors.New("internal error"))

	resp, _ := json.Marshal(err)

	assert.Equal(t, `{"field":"id","message":"bind failed"}`, string(resp))
}

func TestPathParamsBinder(t *testing.T) {
	c := createTestContext("/api/user/999", nil, map[string]string{
		"id":    "1",
		"nr":    "2",
		"slice": "3",
	})
	b := PathParamsBinder(c)

	id := int64(99)
	nr := int64(88)
	var slice = make([]int64, 0)
	var notExisting = make([]int64, 0)
	err := b.Int64("id", &id).
		Int64("nr", &nr).
		Int64s("slice", &slice).
		Int64s("not_existing", &notExisting).
		BindError()

	assert.NoError(t, err)
	assert.Equal(t, int64(1), id)
	assert.Equal(t, int64(2), nr)
	assert.Equal(t, []int64{3}, slice)      // binding params to slice does not make sense but it should not panic either
	assert.Equal(t, []int64{}, notExisting) // binding params to slice does not make sense but it should not panic either
}

func TestQueryParamsBinder_FailFast(t *testing.T) {
	var testCases = []struct {
		name          string
		whenURL       string
		givenFailFast bool
		expectError   []string
	}{
		{
			name:          "ok, FailFast=true stops at first error",
			whenURL:       "/api/user/999?nr=en&id=nope",
			givenFailFast: true,
			expectError: []string{
				`code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing "nope": invalid syntax, field=id`,
			},
		},
		{
			name:          "ok, FailFast=false encounters all errors",
			whenURL:       "/api/user/999?nr=en&id=nope",
			givenFailFast: false,
			expectError: []string{
				`code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing "nope": invalid syntax, field=id`,
				`code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing "en": invalid syntax, field=nr`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, map[string]string{"id": "999"})
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			id := int64(99)
			nr := int64(88)
			errs := b.Int64("id", &id).
				Int64("nr", &nr).
				BindErrors()

			assert.Len(t, errs, len(tc.expectError))
			for _, err := range errs {
				assert.Contains(t, tc.expectError, err.Error())
			}
		})
	}
}

func TestFormFieldBinder(t *testing.T) {
	e := New()
	body := `texta=foo&slice=5`
	req := httptest.NewRequest(http.MethodPost, "/api/search?id=1&nr=2&slice=3&slice=4", strings.NewReader(body))
	req.Header.Set(HeaderContentLength, strconv.Itoa(len(body)))
	req.Header.Set(HeaderContentType, MIMEApplicationForm)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	b := FormFieldBinder(c)

	var texta string
	id := int64(99)
	nr := int64(88)
	var slice = make([]int64, 0)
	var notExisting = make([]int64, 0)
	err := b.
		Int64s("slice", &slice).
		Int64("id", &id).
		Int64("nr", &nr).
		String("texta", &texta).
		Int64s("notExisting", &notExisting).
		BindError()

	assert.NoError(t, err)
	assert.Equal(t, "foo", texta)
	assert.Equal(t, int64(1), id)
	assert.Equal(t, int64(2), nr)
	assert.Equal(t, []int64{5, 3, 4}, slice)
	assert.Equal(t, []int64{}, notExisting)
}

func TestValueBinder_errorStopsBinding(t *testing.T) {
	// this test documents "feature" that binding multiple params can change destination if it was binded before
	// failing parameter binding

	c := createTestContext("/api/user/999?id=1&nr=nope", nil, nil)
	b := QueryParamsBinder(c)

	id := int64(99) // will be changed before nr binding fails
	nr := int64(88) // will not be changed
	err := b.Int64("id", &id).
		Int64("nr", &nr).
		BindError()

	assert.EqualError(t, err, "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=nr")
	assert.Equal(t, int64(1), id)
	assert.Equal(t, int64(88), nr)
}

func TestValueBinder_BindError(t *testing.T) {
	c := createTestContext("/api/user/999?nr=en&id=nope", nil, nil)
	b := QueryParamsBinder(c)

	id := int64(99)
	nr := int64(88)
	err := b.Int64("id", &id).
		Int64("nr", &nr).
		BindError()

	assert.EqualError(t, err, "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=id")
	assert.Nil(t, b.errors)
	assert.Nil(t, b.BindError())
}

func TestValueBinder_GetValues(t *testing.T) {
	var testCases = []struct {
		name           string
		whenValuesFunc func(sourceParam string) []string
		expect         []int64
		expectError    string
	}{
		{
			name:   "ok, default implementation",
			expect: []int64{1, 101},
		},
		{
			name: "ok, values returns nil",
			whenValuesFunc: func(sourceParam string) []string {
				return nil
			},
			expect: []int64(nil),
		},
		{
			name: "ok, values returns empty slice",
			whenValuesFunc: func(sourceParam string) []string {
				return []string{}
			},
			expect: []int64(nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext("/search?nr=en&id=1&id=101", nil, nil)
			b := QueryParamsBinder(c)
			if tc.whenValuesFunc != nil {
				b.ValuesFunc = tc.whenValuesFunc
			}

			var IDs []int64
			err := b.Int64s("id", &IDs).BindError()

			assert.Equal(t, tc.expect, IDs)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_CustomFuncWithError(t *testing.T) {
	c := createTestContext("/search?nr=en&id=1&id=101", nil, nil)
	b := QueryParamsBinder(c)

	id := int64(99)
	givenCustomFunc := func(values []string) []error {
		assert.Equal(t, []string{"1", "101"}, values)

		return []error{
			errors.New("first error"),
			errors.New("second error"),
		}
	}
	err := b.CustomFunc("id", givenCustomFunc).BindError()

	assert.Equal(t, int64(99), id)
	assert.EqualError(t, err, "first error")
}

func TestValueBinder_CustomFunc(t *testing.T) {
	var testCases = []struct {
		name              string
		givenFailFast     bool
		givenFuncErrors   []error
		whenURL           string
		expectParamValues []string
		expectValue       interface{}
		expectErrors      []string
	}{
		{
			name:              "ok, binds value",
			whenURL:           "/search?nr=en&id=1&id=100",
			expectParamValues: []string{"1", "100"},
			expectValue:       int64(1000),
		},
		{
			name:              "ok, params values empty, value is not changed",
			whenURL:           "/search?nr=en",
			expectParamValues: []string{},
			expectValue:       int64(99),
		},
		{
			name:              "nok, previous errors fail fast without binding value",
			givenFailFast:     true,
			whenURL:           "/search?nr=en&id=1&id=100",
			expectParamValues: []string{"1", "100"},
			expectValue:       int64(99),
			expectErrors:      []string{"previous error"},
		},
		{
			name: "nok, func returns errors",
			givenFuncErrors: []error{
				errors.New("first error"),
				errors.New("second error"),
			},
			whenURL:           "/search?nr=en&id=1&id=100",
			expectParamValues: []string{"1", "100"},
			expectValue:       int64(99),
			expectErrors:      []string{"first error", "second error"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			id := int64(99)
			givenCustomFunc := func(values []string) []error {
				assert.Equal(t, tc.expectParamValues, values)
				if tc.givenFuncErrors == nil {
					id = 1000 // emulated conversion and setting value
					return nil
				}
				return tc.givenFuncErrors
			}
			errs := b.CustomFunc("id", givenCustomFunc).BindErrors()

			assert.Equal(t, tc.expectValue, id)
			if tc.expectErrors != nil {
				assert.Len(t, errs, len(tc.expectErrors))
				for _, err := range errs {
					assert.Contains(t, tc.expectErrors, err.Error())
				}
			} else {
				assert.Nil(t, errs)
			}
		})
	}
}

func TestValueBinder_MustCustomFunc(t *testing.T) {
	var testCases = []struct {
		name              string
		givenFailFast     bool
		givenFuncErrors   []error
		whenURL           string
		expectParamValues []string
		expectValue       interface{}
		expectErrors      []string
	}{
		{
			name:              "ok, binds value",
			whenURL:           "/search?nr=en&id=1&id=100",
			expectParamValues: []string{"1", "100"},
			expectValue:       int64(1000),
		},
		{
			name:              "nok, params values empty, returns error, value is not changed",
			whenURL:           "/search?nr=en",
			expectParamValues: []string{},
			expectValue:       int64(99),
			expectErrors:      []string{"code=400, message=required field value is empty, field=id"},
		},
		{
			name:              "nok, previous errors fail fast without binding value",
			givenFailFast:     true,
			whenURL:           "/search?nr=en&id=1&id=100",
			expectParamValues: []string{"1", "100"},
			expectValue:       int64(99),
			expectErrors:      []string{"previous error"},
		},
		{
			name: "nok, func returns errors",
			givenFuncErrors: []error{
				errors.New("first error"),
				errors.New("second error"),
			},
			whenURL:           "/search?nr=en&id=1&id=100",
			expectParamValues: []string{"1", "100"},
			expectValue:       int64(99),
			expectErrors:      []string{"first error", "second error"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			id := int64(99)
			givenCustomFunc := func(values []string) []error {
				assert.Equal(t, tc.expectParamValues, values)
				if tc.givenFuncErrors == nil {
					id = 1000 // emulated conversion and setting value
					return nil
				}
				return tc.givenFuncErrors
			}
			errs := b.MustCustomFunc("id", givenCustomFunc).BindErrors()

			assert.Equal(t, tc.expectValue, id)
			if tc.expectErrors != nil {
				assert.Len(t, errs, len(tc.expectErrors))
				for _, err := range errs {
					assert.Contains(t, tc.expectErrors, err.Error())
				}
			} else {
				assert.Nil(t, errs)
			}
		})
	}
}

func TestValueBinder_String(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     string
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=en&param=de",
			expectValue: "en",
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nr=en",
			expectValue: "default",
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?nr=en&id=1&id=100",
			expectValue:   "default",
			expectError:   "previous error",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=en&param=de",
			expectValue: "en",
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nr=en",
			expectValue: "default",
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?nr=en&id=1&id=100",
			expectValue:   "default",
			expectError:   "previous error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := "default"
			var err error
			if tc.whenMust {
				err = b.MustString("param", &dest).BindError()
			} else {
				err = b.String("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Strings(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []string
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=en&param=de",
			expectValue: []string{"en", "de"},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nr=en",
			expectValue: []string{"default"},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?nr=en&id=1&id=100",
			expectValue:   []string{"default"},
			expectError:   "previous error",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=en&param=de",
			expectValue: []string{"en", "de"},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nr=en",
			expectValue: []string{"default"},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?nr=en&id=1&id=100",
			expectValue:   []string{"default"},
			expectError:   "previous error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := []string{"default"}
			var err error
			if tc.whenMust {
				err = b.MustStrings("param", &dest).BindError()
			} else {
				err = b.Strings("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Int64_intValue(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     int64
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=1&param=100",
			expectValue: 1,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: 99,
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   99,
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: 99,
			expectError: "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1&param=100",
			expectValue: 1,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: 99,
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   99,
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: 99,
			expectError: "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := int64(99)
			var err error
			if tc.whenMust {
				err = b.MustInt64("param", &dest).BindError()
			} else {
				err = b.Int64("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Int_errorMessage(t *testing.T) {
	// int/uint (without byte size) has a little bit different error message so test these separately
	c := createTestContext("/search?param=nope", nil, nil)
	b := QueryParamsBinder(c).FailFast(false)

	destInt := 99
	destUint := uint(98)
	errs := b.Int("param", &destInt).Uint("param", &destUint).BindErrors()

	assert.Equal(t, 99, destInt)
	assert.Equal(t, uint(98), destUint)
	assert.EqualError(t, errs[0], `code=400, message=failed to bind field value to int, internal=strconv.ParseInt: parsing "nope": invalid syntax, field=param`)
	assert.EqualError(t, errs[1], `code=400, message=failed to bind field value to uint, internal=strconv.ParseUint: parsing "nope": invalid syntax, field=param`)
}

func TestValueBinder_Uint64_uintValue(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     uint64
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=1&param=100",
			expectValue: 1,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: 99,
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   99,
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: 99,
			expectError: "code=400, message=failed to bind field value to uint64, internal=strconv.ParseUint: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1&param=100",
			expectValue: 1,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: 99,
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   99,
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: 99,
			expectError: "code=400, message=failed to bind field value to uint64, internal=strconv.ParseUint: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := uint64(99)
			var err error
			if tc.whenMust {
				err = b.MustUint64("param", &dest).BindError()
			} else {
				err = b.Uint64("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Int_Types(t *testing.T) {
	type target struct {
		int64      int64
		mustInt64  int64
		uint64     uint64
		mustUint64 uint64

		int32      int32
		mustInt32  int32
		uint32     uint32
		mustUint32 uint32

		int16      int16
		mustInt16  int16
		uint16     uint16
		mustUint16 uint16

		int8      int8
		mustInt8  int8
		uint8     uint8
		mustUint8 uint8

		byte     byte
		mustByte byte

		int      int
		mustInt  int
		uint     uint
		mustUint uint
	}
	types := []string{
		"int64=1",
		"mustInt64=2",
		"uint64=3",
		"mustUint64=4",

		"int32=5",
		"mustInt32=6",
		"uint32=7",
		"mustUint32=8",

		"int16=9",
		"mustInt16=10",
		"uint16=11",
		"mustUint16=12",

		"int8=13",
		"mustInt8=14",
		"uint8=15",
		"mustUint8=16",

		"byte=17",
		"mustByte=18",

		"int=19",
		"mustInt=20",
		"uint=21",
		"mustUint=22",
	}
	c := createTestContext("/search?"+strings.Join(types, "&"), nil, nil)
	b := QueryParamsBinder(c)

	dest := target{}
	err := b.
		Int64("int64", &dest.int64).
		MustInt64("mustInt64", &dest.mustInt64).
		Uint64("uint64", &dest.uint64).
		MustUint64("mustUint64", &dest.mustUint64).
		Int32("int32", &dest.int32).
		MustInt32("mustInt32", &dest.mustInt32).
		Uint32("uint32", &dest.uint32).
		MustUint32("mustUint32", &dest.mustUint32).
		Int16("int16", &dest.int16).
		MustInt16("mustInt16", &dest.mustInt16).
		Uint16("uint16", &dest.uint16).
		MustUint16("mustUint16", &dest.mustUint16).
		Int8("int8", &dest.int8).
		MustInt8("mustInt8", &dest.mustInt8).
		Uint8("uint8", &dest.uint8).
		MustUint8("mustUint8", &dest.mustUint8).
		Byte("byte", &dest.byte).
		MustByte("mustByte", &dest.mustByte).
		Int("int", &dest.int).
		MustInt("mustInt", &dest.mustInt).
		Uint("uint", &dest.uint).
		MustUint("mustUint", &dest.mustUint).
		BindError()

	assert.NoError(t, err)
	assert.Equal(t, int64(1), dest.int64)
	assert.Equal(t, int64(2), dest.mustInt64)
	assert.Equal(t, uint64(3), dest.uint64)
	assert.Equal(t, uint64(4), dest.mustUint64)

	assert.Equal(t, int32(5), dest.int32)
	assert.Equal(t, int32(6), dest.mustInt32)
	assert.Equal(t, uint32(7), dest.uint32)
	assert.Equal(t, uint32(8), dest.mustUint32)

	assert.Equal(t, int16(9), dest.int16)
	assert.Equal(t, int16(10), dest.mustInt16)
	assert.Equal(t, uint16(11), dest.uint16)
	assert.Equal(t, uint16(12), dest.mustUint16)

	assert.Equal(t, int8(13), dest.int8)
	assert.Equal(t, int8(14), dest.mustInt8)
	assert.Equal(t, uint8(15), dest.uint8)
	assert.Equal(t, uint8(16), dest.mustUint8)

	assert.Equal(t, uint8(17), dest.byte)
	assert.Equal(t, uint8(18), dest.mustByte)

	assert.Equal(t, 19, dest.int)
	assert.Equal(t, 20, dest.mustInt)
	assert.Equal(t, uint(21), dest.uint)
	assert.Equal(t, uint(22), dest.mustUint)
}

func TestValueBinder_Int64s_intsValue(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []int64
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=1&param=2&param=1",
			expectValue: []int64{1, 2, 1},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []int64{99},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []int64{99},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: []int64{99},
			expectError: "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1&param=2&param=1",
			expectValue: []int64{1, 2, 1},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []int64{99},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []int64{99},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []int64{99},
			expectError: "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := []int64{99} // when values are set with bind - contents before bind is gone
			var err error
			if tc.whenMust {
				err = b.MustInt64s("param", &dest).BindError()
			} else {
				err = b.Int64s("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Uint64s_uintsValue(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []uint64
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=1&param=2&param=1",
			expectValue: []uint64{1, 2, 1},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []uint64{99},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []uint64{99},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: []uint64{99},
			expectError: "code=400, message=failed to bind field value to uint64, internal=strconv.ParseUint: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1&param=2&param=1",
			expectValue: []uint64{1, 2, 1},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []uint64{99},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []uint64{99},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []uint64{99},
			expectError: "code=400, message=failed to bind field value to uint64, internal=strconv.ParseUint: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := []uint64{99} // when values are set with bind - contents before bind is gone
			var err error
			if tc.whenMust {
				err = b.MustUint64s("param", &dest).BindError()
			} else {
				err = b.Uint64s("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Ints_Types(t *testing.T) {
	type target struct {
		int64      []int64
		mustInt64  []int64
		uint64     []uint64
		mustUint64 []uint64

		int32      []int32
		mustInt32  []int32
		uint32     []uint32
		mustUint32 []uint32

		int16      []int16
		mustInt16  []int16
		uint16     []uint16
		mustUint16 []uint16

		int8      []int8
		mustInt8  []int8
		uint8     []uint8
		mustUint8 []uint8

		int      []int
		mustInt  []int
		uint     []uint
		mustUint []uint
	}
	types := []string{
		"int64=1",
		"mustInt64=2",
		"uint64=3",
		"mustUint64=4",

		"int32=5",
		"mustInt32=6",
		"uint32=7",
		"mustUint32=8",

		"int16=9",
		"mustInt16=10",
		"uint16=11",
		"mustUint16=12",

		"int8=13",
		"mustInt8=14",
		"uint8=15",
		"mustUint8=16",

		"int=19",
		"mustInt=20",
		"uint=21",
		"mustUint=22",
	}
	url := "/search?"
	for _, v := range types {
		url = url + "&" + v + "&" + v
	}
	c := createTestContext(url, nil, nil)
	b := QueryParamsBinder(c)

	dest := target{}
	err := b.
		Int64s("int64", &dest.int64).
		MustInt64s("mustInt64", &dest.mustInt64).
		Uint64s("uint64", &dest.uint64).
		MustUint64s("mustUint64", &dest.mustUint64).
		Int32s("int32", &dest.int32).
		MustInt32s("mustInt32", &dest.mustInt32).
		Uint32s("uint32", &dest.uint32).
		MustUint32s("mustUint32", &dest.mustUint32).
		Int16s("int16", &dest.int16).
		MustInt16s("mustInt16", &dest.mustInt16).
		Uint16s("uint16", &dest.uint16).
		MustUint16s("mustUint16", &dest.mustUint16).
		Int8s("int8", &dest.int8).
		MustInt8s("mustInt8", &dest.mustInt8).
		Uint8s("uint8", &dest.uint8).
		MustUint8s("mustUint8", &dest.mustUint8).
		Ints("int", &dest.int).
		MustInts("mustInt", &dest.mustInt).
		Uints("uint", &dest.uint).
		MustUints("mustUint", &dest.mustUint).
		BindError()

	assert.NoError(t, err)
	assert.Equal(t, []int64{1, 1}, dest.int64)
	assert.Equal(t, []int64{2, 2}, dest.mustInt64)
	assert.Equal(t, []uint64{3, 3}, dest.uint64)
	assert.Equal(t, []uint64{4, 4}, dest.mustUint64)

	assert.Equal(t, []int32{5, 5}, dest.int32)
	assert.Equal(t, []int32{6, 6}, dest.mustInt32)
	assert.Equal(t, []uint32{7, 7}, dest.uint32)
	assert.Equal(t, []uint32{8, 8}, dest.mustUint32)

	assert.Equal(t, []int16{9, 9}, dest.int16)
	assert.Equal(t, []int16{10, 10}, dest.mustInt16)
	assert.Equal(t, []uint16{11, 11}, dest.uint16)
	assert.Equal(t, []uint16{12, 12}, dest.mustUint16)

	assert.Equal(t, []int8{13, 13}, dest.int8)
	assert.Equal(t, []int8{14, 14}, dest.mustInt8)
	assert.Equal(t, []uint8{15, 15}, dest.uint8)
	assert.Equal(t, []uint8{16, 16}, dest.mustUint8)

	assert.Equal(t, []int{19, 19}, dest.int)
	assert.Equal(t, []int{20, 20}, dest.mustInt)
	assert.Equal(t, []uint{21, 21}, dest.uint)
	assert.Equal(t, []uint{22, 22}, dest.mustUint)
}

func TestValueBinder_Ints_Types_FailFast(t *testing.T) {
	// FailFast() should stop parsing and return early
	errTmpl := "code=400, message=failed to bind field value to %v, internal=strconv.Parse%v: parsing \"nope\": invalid syntax, field=param"
	c := createTestContext("/search?param=1&param=nope&param=2", nil, nil)

	var dest64 []int64
	err := QueryParamsBinder(c).FailFast(true).Int64s("param", &dest64).BindError()
	assert.Equal(t, []int64(nil), dest64)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "int64", "Int"))

	var dest32 []int32
	err = QueryParamsBinder(c).FailFast(true).Int32s("param", &dest32).BindError()
	assert.Equal(t, []int32(nil), dest32)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "int32", "Int"))

	var dest16 []int16
	err = QueryParamsBinder(c).FailFast(true).Int16s("param", &dest16).BindError()
	assert.Equal(t, []int16(nil), dest16)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "int16", "Int"))

	var dest8 []int8
	err = QueryParamsBinder(c).FailFast(true).Int8s("param", &dest8).BindError()
	assert.Equal(t, []int8(nil), dest8)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "int8", "Int"))

	var dest []int
	err = QueryParamsBinder(c).FailFast(true).Ints("param", &dest).BindError()
	assert.Equal(t, []int(nil), dest)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "int", "Int"))

	var destu64 []uint64
	err = QueryParamsBinder(c).FailFast(true).Uint64s("param", &destu64).BindError()
	assert.Equal(t, []uint64(nil), destu64)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "uint64", "Uint"))

	var destu32 []uint32
	err = QueryParamsBinder(c).FailFast(true).Uint32s("param", &destu32).BindError()
	assert.Equal(t, []uint32(nil), destu32)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "uint32", "Uint"))

	var destu16 []uint16
	err = QueryParamsBinder(c).FailFast(true).Uint16s("param", &destu16).BindError()
	assert.Equal(t, []uint16(nil), destu16)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "uint16", "Uint"))

	var destu8 []uint8
	err = QueryParamsBinder(c).FailFast(true).Uint8s("param", &destu8).BindError()
	assert.Equal(t, []uint8(nil), destu8)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "uint8", "Uint"))

	var destu []uint
	err = QueryParamsBinder(c).FailFast(true).Uints("param", &destu).BindError()
	assert.Equal(t, []uint(nil), destu)
	assert.EqualError(t, err, fmt.Sprintf(errTmpl, "uint", "Uint"))
}

func TestValueBinder_Bool(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     bool
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=true&param=1",
			expectValue: true,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: false,
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   false,
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: false,
			expectError: "code=400, message=failed to bind field value to bool, internal=strconv.ParseBool: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1&param=100",
			expectValue: true,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: false,
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   false,
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: false,
			expectError: "code=400, message=failed to bind field value to bool, internal=strconv.ParseBool: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := false
			var err error
			if tc.whenMust {
				err = b.MustBool("param", &dest).BindError()
			} else {
				err = b.Bool("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Bools(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []bool
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=true&param=false&param=1&param=0",
			expectValue: []bool{true, false, true, false},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []bool(nil),
		},
		{
			name:            "nok, previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenURL:         "/search?param=1&param=100",
			expectValue:     []bool(nil),
			expectError:     "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=true&param=nope&param=100",
			expectValue: []bool(nil),
			expectError: "code=400, message=failed to bind field value to bool, internal=strconv.ParseBool: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:          "nok, conversion fails fast, value is not changed",
			givenFailFast: true,
			whenURL:       "/search?param=true&param=nope&param=100",
			expectValue:   []bool(nil),
			expectError:   "code=400, message=failed to bind field value to bool, internal=strconv.ParseBool: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=true&param=false&param=1&param=0",
			expectValue: []bool{true, false, true, false},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []bool(nil),
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:            "nok (must), previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenMust:        true,
			whenURL:         "/search?param=1&param=100",
			expectValue:     []bool(nil),
			expectError:     "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []bool(nil),
			expectError: "code=400, message=failed to bind field value to bool, internal=strconv.ParseBool: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			b.errors = tc.givenBindErrors

			var dest []bool
			var err error
			if tc.whenMust {
				err = b.MustBools("param", &dest).BindError()
			} else {
				err = b.Bools("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Float64(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     float64
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=4.3&param=1",
			expectValue: 4.3,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: 1.123,
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   1.123,
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: 1.123,
			expectError: "code=400, message=failed to bind field value to float64, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=4.3&param=100",
			expectValue: 4.3,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: 1.123,
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   1.123,
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: 1.123,
			expectError: "code=400, message=failed to bind field value to float64, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := 1.123
			var err error
			if tc.whenMust {
				err = b.MustFloat64("param", &dest).BindError()
			} else {
				err = b.Float64("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Float64s(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []float64
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=4.3&param=0",
			expectValue: []float64{4.3, 0},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []float64(nil),
		},
		{
			name:            "nok, previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenURL:         "/search?param=1&param=100",
			expectValue:     []float64(nil),
			expectError:     "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: []float64(nil),
			expectError: "code=400, message=failed to bind field value to float64, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:          "nok, conversion fails fast, value is not changed",
			givenFailFast: true,
			whenURL:       "/search?param=0&param=nope&param=100",
			expectValue:   []float64(nil),
			expectError:   "code=400, message=failed to bind field value to float64, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=4.3&param=0",
			expectValue: []float64{4.3, 0},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []float64(nil),
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:            "nok (must), previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenMust:        true,
			whenURL:         "/search?param=1&param=100",
			expectValue:     []float64(nil),
			expectError:     "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []float64(nil),
			expectError: "code=400, message=failed to bind field value to float64, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			b.errors = tc.givenBindErrors

			var dest []float64
			var err error
			if tc.whenMust {
				err = b.MustFloat64s("param", &dest).BindError()
			} else {
				err = b.Float64s("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Float32(t *testing.T) {
	var testCases = []struct {
		name            string
		givenNoFailFast bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     float32
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=4.3&param=1",
			expectValue: 4.3,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: 1.123,
		},
		{
			name:            "nok, previous errors fail fast without binding value",
			givenNoFailFast: true,
			whenURL:         "/search?param=1&param=100",
			expectValue:     1.123,
			expectError:     "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: 1.123,
			expectError: "code=400, message=failed to bind field value to float32, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=4.3&param=100",
			expectValue: 4.3,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: 1.123,
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:            "nok (must), previous errors fail fast without binding value",
			givenNoFailFast: true,
			whenMust:        true,
			whenURL:         "/search?param=1&param=100",
			expectValue:     1.123,
			expectError:     "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: 1.123,
			expectError: "code=400, message=failed to bind field value to float32, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenNoFailFast)
			if tc.givenNoFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := float32(1.123)
			var err error
			if tc.whenMust {
				err = b.MustFloat32("param", &dest).BindError()
			} else {
				err = b.Float32("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Float32s(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []float32
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=4.3&param=0",
			expectValue: []float32{4.3, 0},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []float32(nil),
		},
		{
			name:            "nok, previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenURL:         "/search?param=1&param=100",
			expectValue:     []float32(nil),
			expectError:     "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: []float32(nil),
			expectError: "code=400, message=failed to bind field value to float32, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:          "nok, conversion fails fast, value is not changed",
			givenFailFast: true,
			whenURL:       "/search?param=0&param=nope&param=100",
			expectValue:   []float32(nil),
			expectError:   "code=400, message=failed to bind field value to float32, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=4.3&param=0",
			expectValue: []float32{4.3, 0},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []float32(nil),
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:            "nok (must), previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenMust:        true,
			whenURL:         "/search?param=1&param=100",
			expectValue:     []float32(nil),
			expectError:     "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []float32(nil),
			expectError: "code=400, message=failed to bind field value to float32, internal=strconv.ParseFloat: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			b.errors = tc.givenBindErrors

			var dest []float32
			var err error
			if tc.whenMust {
				err = b.MustFloat32s("param", &dest).BindError()
			} else {
				err = b.Float32s("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Time(t *testing.T) {
	exampleTime, _ := time.Parse(time.RFC3339, "2020-12-23T09:45:31+02:00")
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		whenLayout      string
		expectValue     time.Time
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=2020-12-23T09:45:31%2B02:00&param=2000-01-02T09:45:31%2B00:00",
			whenLayout:  time.RFC3339,
			expectValue: exampleTime,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=2020-12-23T09:45:31%2B02:00&param=2000-01-02T09:45:31%2B00:00",
			whenLayout:  time.RFC3339,
			expectValue: exampleTime,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := time.Time{}
			var err error
			if tc.whenMust {
				err = b.MustTime("param", &dest, tc.whenLayout).BindError()
			} else {
				err = b.Time("param", &dest, tc.whenLayout).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Times(t *testing.T) {
	exampleTime, _ := time.Parse(time.RFC3339, "2020-12-23T09:45:31+02:00")
	exampleTime2, _ := time.Parse(time.RFC3339, "2000-01-02T09:45:31+00:00")
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		whenLayout      string
		expectValue     []time.Time
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=2020-12-23T09:45:31%2B02:00&param=2000-01-02T09:45:31%2B00:00",
			whenLayout:  time.RFC3339,
			expectValue: []time.Time{exampleTime, exampleTime2},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []time.Time(nil),
		},
		{
			name:            "nok, previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenURL:         "/search?param=1&param=100",
			expectValue:     []time.Time(nil),
			expectError:     "previous error",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=2020-12-23T09:45:31%2B02:00&param=2000-01-02T09:45:31%2B00:00",
			whenLayout:  time.RFC3339,
			expectValue: []time.Time{exampleTime, exampleTime2},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []time.Time(nil),
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:            "nok (must), previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenMust:        true,
			whenURL:         "/search?param=1&param=100",
			expectValue:     []time.Time(nil),
			expectError:     "previous error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			b.errors = tc.givenBindErrors

			layout := time.RFC3339
			if tc.whenLayout != "" {
				layout = tc.whenLayout
			}

			var dest []time.Time
			var err error
			if tc.whenMust {
				err = b.MustTimes("param", &dest, layout).BindError()
			} else {
				err = b.Times("param", &dest, layout).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Duration(t *testing.T) {
	example := 42 * time.Second
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     time.Duration
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=42s&param=1ms",
			expectValue: example,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: 0,
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   0,
			expectError:   "previous error",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=42s&param=1ms",
			expectValue: example,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: 0,
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   0,
			expectError:   "previous error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			var dest time.Duration
			var err error
			if tc.whenMust {
				err = b.MustDuration("param", &dest).BindError()
			} else {
				err = b.Duration("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_Durations(t *testing.T) {
	exampleDuration := 42 * time.Second
	exampleDuration2 := 1 * time.Millisecond
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []time.Duration
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=42s&param=1ms",
			expectValue: []time.Duration{exampleDuration, exampleDuration2},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []time.Duration(nil),
		},
		{
			name:            "nok, previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenURL:         "/search?param=1&param=100",
			expectValue:     []time.Duration(nil),
			expectError:     "previous error",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=42s&param=1ms",
			expectValue: []time.Duration{exampleDuration, exampleDuration2},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []time.Duration(nil),
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:            "nok (must), previous errors fail fast without binding value",
			givenFailFast:   true,
			givenBindErrors: []error{errors.New("previous error")},
			whenMust:        true,
			whenURL:         "/search?param=1&param=100",
			expectValue:     []time.Duration(nil),
			expectError:     "previous error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			b.errors = tc.givenBindErrors

			var dest []time.Duration
			var err error
			if tc.whenMust {
				err = b.MustDurations("param", &dest).BindError()
			} else {
				err = b.Durations("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_BindUnmarshaler(t *testing.T) {
	exampleTime, _ := time.Parse(time.RFC3339, "2020-12-23T09:45:31+02:00")

	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     Timestamp
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=2020-12-23T09:45:31%2B02:00&param=2000-01-02T09:45:31%2B00:00",
			expectValue: Timestamp(exampleTime),
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: Timestamp{},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   Timestamp{},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: Timestamp{},
			expectError: "code=400, message=failed to bind field value to BindUnmarshaler interface, internal=parsing time \"nope\" as \"2006-01-02T15:04:05Z07:00\": cannot parse \"nope\" as \"2006\", field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=2020-12-23T09:45:31%2B02:00&param=2000-01-02T09:45:31%2B00:00",
			expectValue: Timestamp(exampleTime),
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: Timestamp{},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   Timestamp{},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: Timestamp{},
			expectError: "code=400, message=failed to bind field value to BindUnmarshaler interface, internal=parsing time \"nope\" as \"2006-01-02T15:04:05Z07:00\": cannot parse \"nope\" as \"2006\", field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			var dest Timestamp
			var err error
			if tc.whenMust {
				err = b.MustBindUnmarshaler("param", &dest).BindError()
			} else {
				err = b.BindUnmarshaler("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_JSONUnmarshaler(t *testing.T) {
	example := big.NewInt(999)

	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     big.Int
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=999&param=998",
			expectValue: *example,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: big.Int{},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   big.Int{},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=xxx",
			expectValue: big.Int{},
			expectError: "code=400, message=failed to bind field value to json.Unmarshaler interface, internal=math/big: cannot unmarshal \"nope\" into a *big.Int, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=999&param=998",
			expectValue: *example,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: big.Int{},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=xxx",
			expectValue:   big.Int{},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=xxx",
			expectValue: big.Int{},
			expectError: "code=400, message=failed to bind field value to json.Unmarshaler interface, internal=math/big: cannot unmarshal \"nope\" into a *big.Int, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			var dest big.Int
			var err error
			if tc.whenMust {
				err = b.MustJSONUnmarshaler("param", &dest).BindError()
			} else {
				err = b.JSONUnmarshaler("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_TextUnmarshaler(t *testing.T) {
	example := big.NewInt(999)

	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     big.Int
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=999&param=998",
			expectValue: *example,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: big.Int{},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   big.Int{},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=xxx",
			expectValue: big.Int{},
			expectError: "code=400, message=failed to bind field value to encoding.TextUnmarshaler interface, internal=math/big: cannot unmarshal \"nope\" into a *big.Int, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=999&param=998",
			expectValue: *example,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: big.Int{},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=xxx",
			expectValue:   big.Int{},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=xxx",
			expectValue: big.Int{},
			expectError: "code=400, message=failed to bind field value to encoding.TextUnmarshaler interface, internal=math/big: cannot unmarshal \"nope\" into a *big.Int, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			var dest big.Int
			var err error
			if tc.whenMust {
				err = b.MustTextUnmarshaler("param", &dest).BindError()
			} else {
				err = b.TextUnmarshaler("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_BindWithDelimiter_types(t *testing.T) {
	var testCases = []struct {
		name    string
		whenURL string
		expect  interface{}
	}{
		{
			name:   "ok, strings",
			expect: []string{"1", "2", "1"},
		},
		{
			name:   "ok, int64",
			expect: []int64{1, 2, 1},
		},
		{
			name:   "ok, int32",
			expect: []int32{1, 2, 1},
		},
		{
			name:   "ok, int16",
			expect: []int16{1, 2, 1},
		},
		{
			name:   "ok, int8",
			expect: []int8{1, 2, 1},
		},
		{
			name:   "ok, int",
			expect: []int{1, 2, 1},
		},
		{
			name:   "ok, uint64",
			expect: []uint64{1, 2, 1},
		},
		{
			name:   "ok, uint32",
			expect: []uint32{1, 2, 1},
		},
		{
			name:   "ok, uint16",
			expect: []uint16{1, 2, 1},
		},
		{
			name:   "ok, uint8",
			expect: []uint8{1, 2, 1},
		},
		{
			name:   "ok, uint",
			expect: []uint{1, 2, 1},
		},
		{
			name:   "ok, float64",
			expect: []float64{1, 2, 1},
		},
		{
			name:   "ok, float32",
			expect: []float32{1, 2, 1},
		},
		{
			name:    "ok, bool",
			whenURL: "/search?param=1,false&param=true",
			expect:  []bool{true, false, true},
		},
		{
			name:    "ok, Duration",
			whenURL: "/search?param=1s,42s&param=1ms",
			expect:  []time.Duration{1 * time.Second, 42 * time.Second, 1 * time.Millisecond},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			URL := "/search?param=1,2&param=1"
			if tc.whenURL != "" {
				URL = tc.whenURL
			}
			c := createTestContext(URL, nil, nil)
			b := QueryParamsBinder(c)

			switch tc.expect.(type) {
			case []string:
				var dest []string
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []int64:
				var dest []int64
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []int32:
				var dest []int32
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []int16:
				var dest []int16
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []int8:
				var dest []int8
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []int:
				var dest []int
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []uint64:
				var dest []uint64
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []uint32:
				var dest []uint32
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []uint16:
				var dest []uint16
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []uint8:
				var dest []uint8
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []uint:
				var dest []uint
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []float64:
				var dest []float64
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []float32:
				var dest []float32
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []bool:
				var dest []bool
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			case []time.Duration:
				var dest []time.Duration
				assert.NoError(t, b.BindWithDelimiter("param", &dest, ",").BindError())
				assert.Equal(t, tc.expect, dest)
			default:
				assert.Fail(t, "invalid type")
			}
		})
	}
}

func TestValueBinder_BindWithDelimiter(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []int64
		expectError     string
	}{
		{
			name:        "ok, binds value",
			whenURL:     "/search?param=1,2&param=1",
			expectValue: []int64{1, 2, 1},
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: []int64(nil),
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []int64(nil),
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: []int64(nil),
			expectError: "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1,2&param=1",
			expectValue: []int64{1, 2, 1},
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: []int64(nil),
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []int64(nil),
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []int64(nil),
			expectError: "code=400, message=failed to bind field value to int64, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			var dest []int64
			var err error
			if tc.whenMust {
				err = b.MustBindWithDelimiter("param", &dest, ",").BindError()
			} else {
				err = b.BindWithDelimiter("param", &dest, ",").BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBindWithDelimiter_invalidType(t *testing.T) {
	c := createTestContext("/search?param=1&param=100", nil, nil)
	b := QueryParamsBinder(c)

	var dest []BindUnmarshaler
	err := b.BindWithDelimiter("param", &dest, ",").BindError()
	assert.Equal(t, []BindUnmarshaler(nil), dest)
	assert.EqualError(t, err, "code=400, message=unsupported bind type, field=param")
}

func TestValueBinder_UnixTime(t *testing.T) {
	exampleTime, _ := time.Parse(time.RFC3339, "2020-12-28T18:36:43+00:00") // => 1609180603
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     time.Time
		expectError     string
	}{
		{
			name:        "ok, binds value, unix time in seconds",
			whenURL:     "/search?param=1609180603&param=1609180604",
			expectValue: exampleTime,
		},
		{
			name:        "ok, binds value, unix time over int32 value",
			whenURL:     "/search?param=2147483648&param=1609180604",
			expectValue: time.Unix(2147483648, 0),
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1609180603&param=1609180604",
			expectValue: exampleTime,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := time.Time{}
			var err error
			if tc.whenMust {
				err = b.MustUnixTime("param", &dest).BindError()
			} else {
				err = b.UnixTime("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue.UnixNano(), dest.UnixNano())
			assert.Equal(t, tc.expectValue.In(time.UTC), dest.In(time.UTC))
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_UnixTimeMilli(t *testing.T) {
	exampleTime, _ := time.Parse(time.RFC3339Nano, "2022-03-13T15:13:30.140000000+00:00") // => 1647184410140
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     time.Time
		expectError     string
	}{
		{
			name:        "ok, binds value, unix time in milliseconds",
			whenURL:     "/search?param=1647184410140&param=1647184410199",
			expectValue: exampleTime,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1647184410140&param=1647184410199",
			expectValue: exampleTime,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := time.Time{}
			var err error
			if tc.whenMust {
				err = b.MustUnixTimeMilli("param", &dest).BindError()
			} else {
				err = b.UnixTimeMilli("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue.UnixNano(), dest.UnixNano())
			assert.Equal(t, tc.expectValue.In(time.UTC), dest.In(time.UTC))
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_UnixTimeNano(t *testing.T) {
	exampleTime, _ := time.Parse(time.RFC3339, "2020-12-28T18:36:43.000000000+00:00")         // => 1609180603
	exampleTimeNano, _ := time.Parse(time.RFC3339Nano, "2020-12-28T18:36:43.123456789+00:00") // => 1609180603123456789
	exampleTimeNanoBelowSec, _ := time.Parse(time.RFC3339Nano, "1970-01-01T00:00:00.999999999+00:00")
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     time.Time
		expectError     string
	}{
		{
			name:        "ok, binds value, unix time in nano seconds (sec precision)",
			whenURL:     "/search?param=1609180603000000000&param=1609180604",
			expectValue: exampleTime,
		},
		{
			name:        "ok, binds value, unix time in nano seconds",
			whenURL:     "/search?param=1609180603123456789&param=1609180604",
			expectValue: exampleTimeNano,
		},
		{
			name:        "ok, binds value, unix time in nano seconds (below 1 sec)",
			whenURL:     "/search?param=999999999&param=1609180604",
			expectValue: exampleTimeNanoBelowSec,
		},
		{
			name:        "ok, params values empty, value is not changed",
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
		},
		{
			name:          "nok, previous errors fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
		{
			name:        "ok (must), binds value",
			whenMust:    true,
			whenURL:     "/search?param=1609180603000000000&param=1609180604",
			expectValue: exampleTime,
		},
		{
			name:        "ok (must), params values empty, returns error, value is not changed",
			whenMust:    true,
			whenURL:     "/search?nope=1",
			expectValue: time.Time{},
			expectError: "code=400, message=required field value is empty, field=param",
		},
		{
			name:          "nok (must), previous errors fail fast without binding value",
			givenFailFast: true,
			whenMust:      true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   time.Time{},
			expectError:   "previous error",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=strconv.ParseInt: parsing \"nope\": invalid syntax, field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := time.Time{}
			var err error
			if tc.whenMust {
				err = b.MustUnixTimeNano("param", &dest).BindError()
			} else {
				err = b.UnixTimeNano("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue.UnixNano(), dest.UnixNano())
			assert.Equal(t, tc.expectValue.In(time.UTC), dest.In(time.UTC))
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkDefaultBinder_BindInt64_single(b *testing.B) {
	type Opts struct {
		Param int64 `query:"param"`
	}
	c := createTestContext("/search?param=1&param=100", nil, nil)

	b.ReportAllocs()
	b.ResetTimer()
	binder := new(DefaultBinder)
	for i := 0; i < b.N; i++ {
		var dest Opts
		_ = binder.Bind(&dest, c)
	}
}

func BenchmarkValueBinder_BindInt64_single(b *testing.B) {
	c := createTestContext("/search?param=1&param=100", nil, nil)

	b.ReportAllocs()
	b.ResetTimer()
	type Opts struct {
		Param int64
	}
	binder := QueryParamsBinder(c)
	for i := 0; i < b.N; i++ {
		var dest Opts
		_ = binder.Int64("param", &dest.Param).BindError()
	}
}

func BenchmarkRawFunc_Int64_single(b *testing.B) {
	c := createTestContext("/search?param=1&param=100", nil, nil)

	rawFunc := func(input string, defaultValue int64) (int64, bool) {
		if input == "" {
			return defaultValue, true
		}
		n, err := strconv.Atoi(input)
		if err != nil {
			return 0, false
		}
		return int64(n), true
	}

	b.ReportAllocs()
	b.ResetTimer()
	type Opts struct {
		Param int64
	}
	for i := 0; i < b.N; i++ {
		var dest Opts
		if n, ok := rawFunc(c.QueryParam("param"), 1); ok {
			dest.Param = n
		}
	}
}

func BenchmarkDefaultBinder_BindInt64_10_fields(b *testing.B) {
	type Opts struct {
		Int64  int64  `query:"int64"`
		Int32  int32  `query:"int32"`
		Int16  int16  `query:"int16"`
		Int8   int8   `query:"int8"`
		String string `query:"string"`

		Uint64  uint64   `query:"uint64"`
		Uint32  uint32   `query:"uint32"`
		Uint16  uint16   `query:"uint16"`
		Uint8   uint8    `query:"uint8"`
		Strings []string `query:"strings"`
	}
	c := createTestContext("/search?int64=1&int32=2&int16=3&int8=4&string=test&uint64=5&uint32=6&uint16=7&uint8=8&strings=first&strings=second", nil, nil)

	b.ReportAllocs()
	b.ResetTimer()
	binder := new(DefaultBinder)
	for i := 0; i < b.N; i++ {
		var dest Opts
		_ = binder.Bind(&dest, c)
		if dest.Int64 != 1 {
			b.Fatalf("int64!=1")
		}
	}
}

func BenchmarkValueBinder_BindInt64_10_fields(b *testing.B) {
	type Opts struct {
		Int64  int64  `query:"int64"`
		Int32  int32  `query:"int32"`
		Int16  int16  `query:"int16"`
		Int8   int8   `query:"int8"`
		String string `query:"string"`

		Uint64  uint64   `query:"uint64"`
		Uint32  uint32   `query:"uint32"`
		Uint16  uint16   `query:"uint16"`
		Uint8   uint8    `query:"uint8"`
		Strings []string `query:"strings"`
	}
	c := createTestContext("/search?int64=1&int32=2&int16=3&int8=4&string=test&uint64=5&uint32=6&uint16=7&uint8=8&strings=first&strings=second", nil, nil)

	b.ReportAllocs()
	b.ResetTimer()
	binder := QueryParamsBinder(c)
	for i := 0; i < b.N; i++ {
		var dest Opts
		_ = binder.
			Int64("int64", &dest.Int64).
			Int32("int32", &dest.Int32).
			Int16("int16", &dest.Int16).
			Int8("int8", &dest.Int8).
			String("string", &dest.String).
			Uint64("int64", &dest.Uint64).
			Uint32("int32", &dest.Uint32).
			Uint16("int16", &dest.Uint16).
			Uint8("int8", &dest.Uint8).
			Strings("strings", &dest.Strings).
			BindError()
		if dest.Int64 != 1 {
			b.Fatalf("int64!=1")
		}
	}
}

func TestValueBinder_TimeError(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		whenLayout      string
		expectValue     time.Time
		expectError     string
	}{
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=parsing time \"nope\": extra text: \"nope\", field=param",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: time.Time{},
			expectError: "code=400, message=failed to bind field value to Time, internal=parsing time \"nope\": extra text: \"nope\", field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			dest := time.Time{}
			var err error
			if tc.whenMust {
				err = b.MustTime("param", &dest, tc.whenLayout).BindError()
			} else {
				err = b.Time("param", &dest, tc.whenLayout).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_TimesError(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		whenLayout      string
		expectValue     []time.Time
		expectError     string
	}{
		{
			name:          "nok, fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []time.Time(nil),
			expectError:   "code=400, message=failed to bind field value to Time, internal=parsing time \"1\" as \"2006-01-02T15:04:05Z07:00\": cannot parse \"1\" as \"2006\", field=param",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: []time.Time(nil),
			expectError: "code=400, message=failed to bind field value to Time, internal=parsing time \"nope\" as \"2006-01-02T15:04:05Z07:00\": cannot parse \"nope\" as \"2006\", field=param",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []time.Time(nil),
			expectError: "code=400, message=failed to bind field value to Time, internal=parsing time \"nope\" as \"2006-01-02T15:04:05Z07:00\": cannot parse \"nope\" as \"2006\", field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			b.errors = tc.givenBindErrors

			layout := time.RFC3339
			if tc.whenLayout != "" {
				layout = tc.whenLayout
			}

			var dest []time.Time
			var err error
			if tc.whenMust {
				err = b.MustTimes("param", &dest, layout).BindError()
			} else {
				err = b.Times("param", &dest, layout).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_DurationError(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     time.Duration
		expectError     string
	}{
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: 0,
			expectError: "code=400, message=failed to bind field value to Duration, internal=time: invalid duration \"nope\", field=param",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: 0,
			expectError: "code=400, message=failed to bind field value to Duration, internal=time: invalid duration \"nope\", field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			if tc.givenFailFast {
				b.errors = []error{errors.New("previous error")}
			}

			var dest time.Duration
			var err error
			if tc.whenMust {
				err = b.MustDuration("param", &dest).BindError()
			} else {
				err = b.Duration("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValueBinder_DurationsError(t *testing.T) {
	var testCases = []struct {
		name            string
		givenFailFast   bool
		givenBindErrors []error
		whenURL         string
		whenMust        bool
		expectValue     []time.Duration
		expectError     string
	}{
		{
			name:          "nok, fail fast without binding value",
			givenFailFast: true,
			whenURL:       "/search?param=1&param=100",
			expectValue:   []time.Duration(nil),
			expectError:   "code=400, message=failed to bind field value to Duration, internal=time: missing unit in duration \"1\", field=param",
		},
		{
			name:        "nok, conversion fails, value is not changed",
			whenURL:     "/search?param=nope&param=100",
			expectValue: []time.Duration(nil),
			expectError: "code=400, message=failed to bind field value to Duration, internal=time: invalid duration \"nope\", field=param",
		},
		{
			name:        "nok (must), conversion fails, value is not changed",
			whenMust:    true,
			whenURL:     "/search?param=nope&param=100",
			expectValue: []time.Duration(nil),
			expectError: "code=400, message=failed to bind field value to Duration, internal=time: invalid duration \"nope\", field=param",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := createTestContext(tc.whenURL, nil, nil)
			b := QueryParamsBinder(c).FailFast(tc.givenFailFast)
			b.errors = tc.givenBindErrors

			var dest []time.Duration
			var err error
			if tc.whenMust {
				err = b.MustDurations("param", &dest).BindError()
			} else {
				err = b.Durations("param", &dest).BindError()
			}

			assert.Equal(t, tc.expectValue, dest)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
