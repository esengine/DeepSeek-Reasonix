// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TextUnmarshalerType implements encoding.TextUnmarshaler but NOT BindUnmarshaler
type TextUnmarshalerType struct {
	Value string
}

func (t *TextUnmarshalerType) UnmarshalText(data []byte) error {
	s := string(data)
	if s == "invalid" {
		return fmt.Errorf("invalid value: %s", s)
	}
	t.Value = strings.ToUpper(s)
	return nil
}

// JSONUnmarshalerType implements json.Unmarshaler but NOT BindUnmarshaler or TextUnmarshaler
type JSONUnmarshalerType struct {
	Value string
}

func (j *JSONUnmarshalerType) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &j.Value)
}

func TestPathParam(t *testing.T) {
	var testCases = []struct {
		name       string
		givenKey   string
		givenValue string
		expect     bool
		expectErr  string
	}{
		{
			name:       "ok",
			givenValue: "true",
			expect:     true,
		},
		{
			name:       "nok, non existent key",
			givenKey:   "missing",
			givenValue: "true",
			expect:     false,
			expectErr:  ErrNonExistentKey.Error(),
		},
		{
			name:       "nok, invalid value",
			givenValue: "can_parse_me",
			expect:     false,
			expectErr:  `code=400, message=path param, internal=failed to parse value, err: strconv.ParseBool: parsing "can_parse_me": invalid syntax, field=key`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			c := e.NewContext(nil, nil)
			c.SetParamNames(cmp.Or(tc.givenKey, "key"))
			c.SetParamValues(tc.givenValue)

			v, err := PathParam[bool](c, "key")
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestPathParam_UnsupportedType(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)
	c.SetParamNames("key")
	c.SetParamValues("true")

	v, err := PathParam[[]bool](c, "key")

	expectErr := "code=400, message=path param, internal=failed to parse value, err: unsupported value type: *[]bool, field=key"
	assert.EqualError(t, err, expectErr)
	assert.Equal(t, []bool(nil), v)
}

func TestQueryParam(t *testing.T) {
	var testCases = []struct {
		name      string
		givenURL  string
		expect    bool
		expectErr string
	}{
		{
			name:     "ok",
			givenURL: "/?key=true",
			expect:   true,
		},
		{
			name:      "nok, non existent key",
			givenURL:  "/?different=true",
			expect:    false,
			expectErr: ErrNonExistentKey.Error(),
		},
		{
			name:      "nok, invalid value",
			givenURL:  "/?key=invalidbool",
			expect:    false,
			expectErr: `code=400, message=query param, internal=failed to parse value, err: strconv.ParseBool: parsing "invalidbool": invalid syntax, field=key`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := QueryParam[bool](c, "key")
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestQueryParam_UnsupportedType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/?key=bool", nil)
	e := New()
	c := e.NewContext(req, nil)

	v, err := QueryParam[[]bool](c, "key")

	expectErr := "code=400, message=query param, internal=failed to parse value, err: unsupported value type: *[]bool, field=key"
	assert.EqualError(t, err, expectErr)
	assert.Equal(t, []bool(nil), v)
}

func TestQueryParams(t *testing.T) {
	var testCases = []struct {
		name      string
		givenURL  string
		expect    []bool
		expectErr string
	}{
		{
			name:     "ok",
			givenURL: "/?key=true&key=false",
			expect:   []bool{true, false},
		},
		{
			name:      "nok, non existent key",
			givenURL:  "/?different=true",
			expect:    []bool(nil),
			expectErr: ErrNonExistentKey.Error(),
		},
		{
			name:      "nok, invalid value",
			givenURL:  "/?key=true&key=invalidbool",
			expect:    []bool(nil),
			expectErr: `code=400, message=query params, internal=failed to parse value, err: strconv.ParseBool: parsing "invalidbool": invalid syntax, field=key`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := QueryParams[bool](c, "key")
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestQueryParams_UnsupportedType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/?key=bool", nil)
	e := New()
	c := e.NewContext(req, nil)

	v, err := QueryParams[[]bool](c, "key")

	expectErr := "code=400, message=query params, internal=failed to parse value, err: unsupported value type: *[]bool, field=key"
	assert.EqualError(t, err, expectErr)
	assert.Equal(t, [][]bool(nil), v)
}

func TestFormValue(t *testing.T) {
	var testCases = []struct {
		name      string
		givenURL  string
		expect    bool
		expectErr string
	}{
		{
			name:     "ok",
			givenURL: "/?key=true",
			expect:   true,
		},
		{
			name:      "nok, non existent key",
			givenURL:  "/?different=true",
			expect:    false,
			expectErr: ErrNonExistentKey.Error(),
		},
		{
			name:      "nok, invalid value",
			givenURL:  "/?key=invalidbool",
			expect:    false,
			expectErr: `code=400, message=form param, internal=failed to parse value, err: strconv.ParseBool: parsing "invalidbool": invalid syntax, field=key`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := FormParam[bool](c, "key")
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestFormValue_UnsupportedType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/?key=bool", nil)
	e := New()
	c := e.NewContext(req, nil)

	v, err := FormParam[[]bool](c, "key")

	expectErr := "code=400, message=form param, internal=failed to parse value, err: unsupported value type: *[]bool, field=key"
	assert.EqualError(t, err, expectErr)
	assert.Equal(t, []bool(nil), v)
}

func TestFormValues(t *testing.T) {
	var testCases = []struct {
		name      string
		givenURL  string
		expect    []bool
		expectErr string
	}{
		{
			name:     "ok",
			givenURL: "/?key=true&key=false",
			expect:   []bool{true, false},
		},
		{
			name:      "nok, non existent key",
			givenURL:  "/?different=true",
			expect:    []bool(nil),
			expectErr: ErrNonExistentKey.Error(),
		},
		{
			name:      "nok, invalid value",
			givenURL:  "/?key=true&key=invalidbool",
			expect:    []bool(nil),
			expectErr: `code=400, message=form params, internal=failed to parse value, err: strconv.ParseBool: parsing "invalidbool": invalid syntax, field=key`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := FormParams[bool](c, "key")
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestFormValues_UnsupportedType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/?key=bool", nil)
	e := New()
	c := e.NewContext(req, nil)

	v, err := FormParams[[]bool](c, "key")

	expectErr := "code=400, message=form params, internal=failed to parse value, err: unsupported value type: *[]bool, field=key"
	assert.EqualError(t, err, expectErr)
	assert.Equal(t, [][]bool(nil), v)
}

func TestParseValue_bool(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    bool
		expectErr error
	}{
		{
			name:   "ok, true",
			when:   "true",
			expect: true,
		},
		{
			name:   "ok, false",
			when:   "false",
			expect: false,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: true,
		},
		{
			name:   "ok, 0",
			when:   "0",
			expect: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[bool](tc.when)
			if tc.expectErr != nil {
				assert.ErrorIs(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_float32(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    float32
		expectErr string
	}{
		{
			name:   "ok, 123.345",
			when:   "123.345",
			expect: 123.345,
		},
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, Inf",
			when:   "+Inf",
			expect: float32(math.Inf(1)),
		},
		{
			name:   "ok, Inf",
			when:   "-Inf",
			expect: float32(math.Inf(-1)),
		},
		{
			name:   "ok, NaN",
			when:   "NaN",
			expect: float32(math.NaN()),
		},
		{
			name:      "ok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseFloat: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[float32](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			if math.IsNaN(float64(tc.expect)) {
				if !math.IsNaN(float64(v)) {
					t.Fatal("expected NaN but got non NaN")
				}
			} else {
				assert.Equal(t, tc.expect, v)
			}
		})
	}
}

func TestParseValue_float64(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    float64
		expectErr string
	}{
		{
			name:   "ok, 123.345",
			when:   "123.345",
			expect: 123.345,
		},
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, Inf",
			when:   "+Inf",
			expect: math.Inf(1),
		},
		{
			name:   "ok, Inf",
			when:   "-Inf",
			expect: math.Inf(-1),
		},
		{
			name:   "ok, NaN",
			when:   "NaN",
			expect: math.NaN(),
		},
		{
			name:      "ok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseFloat: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[float64](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			if math.IsNaN(tc.expect) {
				if !math.IsNaN(v) {
					t.Fatal("expected NaN but got non NaN")
				}
			} else {
				assert.Equal(t, tc.expect, v)
			}
		})
	}
}

func TestParseValue_int(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    int
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, -1",
			when:   "-1",
			expect: -1,
		},
		{
			name:   "ok, max int (64bit)",
			when:   "9223372036854775807",
			expect: 9223372036854775807,
		},
		{
			name:   "ok, min int (64bit)",
			when:   "-9223372036854775808",
			expect: -9223372036854775808,
		},
		{
			name:      "ok, overflow max int (64bit)",
			when:      "9223372036854775808",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "9223372036854775808": value out of range`,
		},
		{
			name:      "ok, underflow min int (64bit)",
			when:      "-9223372036854775809",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "-9223372036854775809": value out of range`,
		},
		{
			name:      "ok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[int](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_uint(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    uint
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, max uint (64bit)",
			when:   "18446744073709551615",
			expect: 18446744073709551615,
		},
		{
			name:      "nok, overflow max uint (64bit)",
			when:      "18446744073709551616",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "18446744073709551616": value out of range`,
		},
		{
			name:      "nok, negative value",
			when:      "-1",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "-1": invalid syntax`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[uint](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_int8(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    int8
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, -1",
			when:   "-1",
			expect: -1,
		},
		{
			name:   "ok, max int8",
			when:   "127",
			expect: 127,
		},
		{
			name:   "ok, min int8",
			when:   "-128",
			expect: -128,
		},
		{
			name:      "nok, overflow max int8",
			when:      "128",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "128": value out of range`,
		},
		{
			name:      "nok, underflow min int8",
			when:      "-129",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "-129": value out of range`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[int8](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_int16(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    int16
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, -1",
			when:   "-1",
			expect: -1,
		},
		{
			name:   "ok, max int16",
			when:   "32767",
			expect: 32767,
		},
		{
			name:   "ok, min int16",
			when:   "-32768",
			expect: -32768,
		},
		{
			name:      "nok, overflow max int16",
			when:      "32768",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "32768": value out of range`,
		},
		{
			name:      "nok, underflow min int16",
			when:      "-32769",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "-32769": value out of range`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[int16](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_int32(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    int32
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, -1",
			when:   "-1",
			expect: -1,
		},
		{
			name:   "ok, max int32",
			when:   "2147483647",
			expect: 2147483647,
		},
		{
			name:   "ok, min int32",
			when:   "-2147483648",
			expect: -2147483648,
		},
		{
			name:      "nok, overflow max int32",
			when:      "2147483648",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "2147483648": value out of range`,
		},
		{
			name:      "nok, underflow min int32",
			when:      "-2147483649",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "-2147483649": value out of range`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[int32](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_int64(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    int64
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, -1",
			when:   "-1",
			expect: -1,
		},
		{
			name:   "ok, max int64",
			when:   "9223372036854775807",
			expect: 9223372036854775807,
		},
		{
			name:   "ok, min int64",
			when:   "-9223372036854775808",
			expect: -9223372036854775808,
		},
		{
			name:      "nok, overflow max int64",
			when:      "9223372036854775808",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "9223372036854775808": value out of range`,
		},
		{
			name:      "nok, underflow min int64",
			when:      "-9223372036854775809",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "-9223372036854775809": value out of range`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseInt: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[int64](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_uint8(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    uint8
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, max uint8",
			when:   "255",
			expect: 255,
		},
		{
			name:      "nok, overflow max uint8",
			when:      "256",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "256": value out of range`,
		},
		{
			name:      "nok, negative value",
			when:      "-1",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "-1": invalid syntax`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[uint8](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_uint16(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    uint16
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, max uint16",
			when:   "65535",
			expect: 65535,
		},
		{
			name:      "nok, overflow max uint16",
			when:      "65536",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "65536": value out of range`,
		},
		{
			name:      "nok, negative value",
			when:      "-1",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "-1": invalid syntax`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[uint16](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_uint32(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    uint32
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, max uint32",
			when:   "4294967295",
			expect: 4294967295,
		},
		{
			name:      "nok, overflow max uint32",
			when:      "4294967296",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "4294967296": value out of range`,
		},
		{
			name:      "nok, negative value",
			when:      "-1",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "-1": invalid syntax`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[uint32](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_uint64(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    uint64
		expectErr string
	}{
		{
			name:   "ok, 0",
			when:   "0",
			expect: 0,
		},
		{
			name:   "ok, 1",
			when:   "1",
			expect: 1,
		},
		{
			name:   "ok, max uint64",
			when:   "18446744073709551615",
			expect: 18446744073709551615,
		},
		{
			name:      "nok, overflow max uint64",
			when:      "18446744073709551616",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "18446744073709551616": value out of range`,
		},
		{
			name:      "nok, negative value",
			when:      "-1",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "-1": invalid syntax`,
		},
		{
			name:      "nok, invalid value",
			when:      "X",
			expect:    0,
			expectErr: `failed to parse value, err: strconv.ParseUint: parsing "X": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[uint64](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_string(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    string
		expectErr string
	}{
		{
			name:   "ok, my",
			when:   "my",
			expect: "my",
		},
		{
			name:   "ok, empty",
			when:   "",
			expect: "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[string](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_Duration(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    time.Duration
		expectErr string
	}{
		{
			name:   "ok, 10h11m01s",
			when:   "10h11m01s",
			expect: 10*time.Hour + 11*time.Minute + 1*time.Second,
		},
		{
			name:   "ok, empty",
			when:   "",
			expect: 0,
		},
		{
			name:      "ok, invalid",
			when:      "0x0",
			expect:    0,
			expectErr: `failed to parse value, err: time: unknown unit "x" in duration "0x0"`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[time.Duration](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_Time(t *testing.T) {
	tallinn, err := time.LoadLocation("Europe/Tallinn")
	if err != nil {
		t.Fatal(err)
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	parse := func(t *testing.T, layout string, s string) time.Time {
		result, err := time.Parse(layout, s)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	parseInLoc := func(t *testing.T, layout string, s string, loc *time.Location) time.Time {
		result, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	var testCases = []struct {
		name         string
		when         string
		whenLayout   TimeLayout
		whenTimeOpts *TimeOpts
		expect       time.Time
		expectErr    string
	}{
		{
			name:   "ok, defaults to RFC3339Nano",
			when:   "2006-01-02T15:04:05.999999999Z",
			expect: parse(t, time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z"),
		},
		{
			name: "ok, custom TimeOpt",
			when: "2006-01-02",
			whenTimeOpts: &TimeOpts{
				Layout:          time.DateOnly,
				ParseInLocation: tallinn,
				ToInLocation:    berlin,
			},
			expect: parseInLoc(t, time.DateTime, "2006-01-01 23:00:00", berlin),
		},
		{
			name:       "ok, custom layout",
			when:       "2006-01-02",
			whenLayout: TimeLayout(time.DateOnly),
			expect:     parse(t, time.DateOnly, "2006-01-02"),
		},
		{
			name:       "ok, TimeLayoutUnixTime",
			when:       "1766604665",
			whenLayout: TimeLayoutUnixTime,
			expect:     parse(t, time.RFC3339Nano, "2025-12-24T19:31:05Z"),
		},
		{
			name:       "nok, TimeLayoutUnixTime, invalid value",
			when:       "176x6604665",
			whenLayout: TimeLayoutUnixTime,
			expectErr:  `failed to parse value, err: strconv.ParseInt: parsing "176x6604665": invalid syntax`,
		},
		{
			name:       "ok, TimeLayoutUnixTimeMilli",
			when:       "1766604665123",
			whenLayout: TimeLayoutUnixTimeMilli,
			expect:     parse(t, time.RFC3339Nano, "2025-12-24T19:31:05.123Z"),
		},
		{
			name:       "nok, TimeLayoutUnixTimeMilli, invalid value",
			when:       "1x766604665123",
			whenLayout: TimeLayoutUnixTimeMilli,
			expectErr:  `failed to parse value, err: strconv.ParseInt: parsing "1x766604665123": invalid syntax`,
		},
		{
			name:       "ok, TimeLayoutUnixTimeMilli",
			when:       "1766604665999999999",
			whenLayout: TimeLayoutUnixTimeNano,
			expect:     parse(t, time.RFC3339Nano, "2025-12-24T19:31:05.999999999Z"),
		},
		{
			name:       "nok, TimeLayoutUnixTimeMilli, invalid value",
			when:       "1x766604665999999999",
			whenLayout: TimeLayoutUnixTimeNano,
			expectErr:  `failed to parse value, err: strconv.ParseInt: parsing "1x766604665999999999": invalid syntax`,
		},
		{
			name:      "ok, invalid",
			when:      "xx",
			expect:    time.Time{},
			expectErr: `failed to parse value, err: parsing time "xx" as "2006-01-02T15:04:05.999999999Z07:00": cannot parse "xx" as "2006"`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var opts []any
			if tc.whenLayout != "" {
				opts = append(opts, tc.whenLayout)
			}
			if tc.whenTimeOpts != nil {
				opts = append(opts, *tc.whenTimeOpts)
			}
			v, err := ParseValue[time.Time](tc.when, opts...)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_OptionsOnlyForTime(t *testing.T) {
	_, err := ParseValue[int]("test", TimeLayoutUnixTime)
	assert.EqualError(t, err, `failed to parse value, err: options are only supported for time.Time, got *int`)
}

func TestParseValue_BindUnmarshaler(t *testing.T) {
	exampleTime, _ := time.Parse(time.RFC3339, "2020-12-23T09:45:31+02:00")

	var testCases = []struct {
		name      string
		when      string
		expect    Timestamp
		expectErr string
	}{
		{
			name:   "ok",
			when:   "2020-12-23T09:45:31+02:00",
			expect: Timestamp(exampleTime),
		},
		{
			name:      "nok, invalid value",
			when:      "2020-12-23T09:45:3102:00",
			expect:    Timestamp{},
			expectErr: `failed to parse value, err: parsing time "2020-12-23T09:45:3102:00" as "2006-01-02T15:04:05Z07:00": cannot parse "02:00" as "Z07:00"`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[Timestamp](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_TextUnmarshaler(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    TextUnmarshalerType
		expectErr string
	}{
		{
			name:   "ok, converts to uppercase",
			when:   "hello",
			expect: TextUnmarshalerType{Value: "HELLO"},
		},
		{
			name:   "ok, empty string",
			when:   "",
			expect: TextUnmarshalerType{Value: ""},
		},
		{
			name:      "nok, invalid value",
			when:      "invalid",
			expect:    TextUnmarshalerType{},
			expectErr: "failed to parse value, err: invalid value: invalid",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[TextUnmarshalerType](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValue_JSONUnmarshaler(t *testing.T) {
	var testCases = []struct {
		name      string
		when      string
		expect    JSONUnmarshalerType
		expectErr string
	}{
		{
			name:   "ok, valid JSON string",
			when:   `"hello"`,
			expect: JSONUnmarshalerType{Value: "hello"},
		},
		{
			name:   "ok, empty JSON string",
			when:   `""`,
			expect: JSONUnmarshalerType{Value: ""},
		},
		{
			name:      "nok, invalid JSON",
			when:      "not-json",
			expect:    JSONUnmarshalerType{},
			expectErr: "failed to parse value, err: invalid character 'o' in literal null (expecting 'u')",
		},
		{
			name:      "nok, unquoted string",
			when:      "hello",
			expect:    JSONUnmarshalerType{},
			expectErr: "failed to parse value, err: invalid character 'h' looking for beginning of value",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValue[JSONUnmarshalerType](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestParseValues_bools(t *testing.T) {
	var testCases = []struct {
		name      string
		when      []string
		expect    []bool
		expectErr string
	}{
		{
			name:   "ok",
			when:   []string{"true", "0", "false", "1"},
			expect: []bool{true, false, false, true},
		},
		{
			name:      "nok",
			when:      []string{"true", "10"},
			expect:    nil,
			expectErr: `failed to parse value, err: strconv.ParseBool: parsing "10": invalid syntax`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseValues[bool](tc.when)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestPathParamOr(t *testing.T) {
	var testCases = []struct {
		name         string
		givenKey     string
		givenValue   string
		defaultValue int
		expect       int
		expectErr    string
	}{
		{
			name:         "ok, param exists",
			givenKey:     "id",
			givenValue:   "123",
			defaultValue: 999,
			expect:       123,
		},
		{
			name:         "ok, param missing - returns default",
			givenKey:     "other",
			givenValue:   "123",
			defaultValue: 999,
			expect:       999,
		},
		{
			name:         "ok, param exists but empty - returns default",
			givenKey:     "id",
			givenValue:   "",
			defaultValue: 999,
			expect:       999,
		},
		{
			name:         "nok, invalid value",
			givenKey:     "id",
			givenValue:   "invalid",
			defaultValue: 999,
			expectErr:    "code=400, message=path param, internal=failed to parse value",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			c := e.NewContext(nil, nil)
			c.SetParamNames(tc.givenKey)
			c.SetParamValues(tc.givenValue)

			v, err := PathParamOr[int](c, "id", tc.defaultValue)
			if tc.expectErr != "" {
				assert.ErrorContains(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestQueryParamOr(t *testing.T) {
	var testCases = []struct {
		name         string
		givenURL     string
		defaultValue int
		expect       int
		expectErr    string
	}{
		{
			name:         "ok, param exists",
			givenURL:     "/?key=42",
			defaultValue: 999,
			expect:       42,
		},
		{
			name:         "ok, param missing - returns default",
			givenURL:     "/?other=42",
			defaultValue: 999,
			expect:       999,
		},
		{
			name:         "ok, param exists but empty - returns default",
			givenURL:     "/?key=",
			defaultValue: 999,
			expect:       999,
		},
		{
			name:         "nok, invalid value",
			givenURL:     "/?key=invalid",
			defaultValue: 999,
			expectErr:    "code=400, message=query param",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := QueryParamOr[int](c, "key", tc.defaultValue)
			if tc.expectErr != "" {
				assert.ErrorContains(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestQueryParamsOr(t *testing.T) {
	var testCases = []struct {
		name         string
		givenURL     string
		defaultValue []int
		expect       []int
		expectErr    string
	}{
		{
			name:         "ok, params exist",
			givenURL:     "/?key=1&key=2&key=3",
			defaultValue: []int{999},
			expect:       []int{1, 2, 3},
		},
		{
			name:         "ok, params missing - returns default",
			givenURL:     "/?other=1",
			defaultValue: []int{7, 8, 9},
			expect:       []int{7, 8, 9},
		},
		{
			name:         "nok, invalid value",
			givenURL:     "/?key=1&key=invalid",
			defaultValue: []int{999},
			expectErr:    "code=400, message=query params",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := QueryParamsOr[int](c, "key", tc.defaultValue)
			if tc.expectErr != "" {
				assert.ErrorContains(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestFormValueOr(t *testing.T) {
	var testCases = []struct {
		name         string
		givenURL     string
		defaultValue string
		expect       string
		expectErr    string
	}{
		{
			name:         "ok, value exists",
			givenURL:     "/?name=john",
			defaultValue: "default",
			expect:       "john",
		},
		{
			name:         "ok, value missing - returns default",
			givenURL:     "/?other=john",
			defaultValue: "default",
			expect:       "default",
		},
		{
			name:         "ok, value exists but empty - returns default",
			givenURL:     "/?name=",
			defaultValue: "default",
			expect:       "default",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := FormParamOr[string](c, "name", tc.defaultValue)
			if tc.expectErr != "" {
				assert.ErrorContains(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}

func TestFormValuesOr(t *testing.T) {
	var testCases = []struct {
		name         string
		givenURL     string
		defaultValue []string
		expect       []string
		expectErr    string
	}{
		{
			name:         "ok, values exist",
			givenURL:     "/?tags=go&tags=rust&tags=python",
			defaultValue: []string{"default"},
			expect:       []string{"go", "rust", "python"},
		},
		{
			name:         "ok, values missing - returns default",
			givenURL:     "/?other=value",
			defaultValue: []string{"a", "b"},
			expect:       []string{"a", "b"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.givenURL, nil)
			e := New()
			c := e.NewContext(req, nil)

			v, err := FormParamsOr[string](c, "tags", tc.defaultValue)
			if tc.expectErr != "" {
				assert.ErrorContains(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, v)
		})
	}
}
