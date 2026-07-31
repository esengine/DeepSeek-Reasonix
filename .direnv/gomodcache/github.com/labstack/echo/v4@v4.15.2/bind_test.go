// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type bindTestStruct struct {
	I           int
	PtrI        *int
	I8          int8
	PtrI8       *int8
	I16         int16
	PtrI16      *int16
	I32         int32
	PtrI32      *int32
	I64         int64
	PtrI64      *int64
	UI          uint
	PtrUI       *uint
	UI8         uint8
	PtrUI8      *uint8
	UI16        uint16
	PtrUI16     *uint16
	UI32        uint32
	PtrUI32     *uint32
	UI64        uint64
	PtrUI64     *uint64
	B           bool
	PtrB        *bool
	F32         float32
	PtrF32      *float32
	F64         float64
	PtrF64      *float64
	S           string
	PtrS        *string
	cantSet     string
	DoesntExist string
	GoT         time.Time
	GoTptr      *time.Time
	T           Timestamp
	Tptr        *Timestamp
	SA          StringArray
}

type bindTestStructWithTags struct {
	I           int      `json:"I" form:"I"`
	PtrI        *int     `json:"PtrI" form:"PtrI"`
	I8          int8     `json:"I8" form:"I8"`
	PtrI8       *int8    `json:"PtrI8" form:"PtrI8"`
	I16         int16    `json:"I16" form:"I16"`
	PtrI16      *int16   `json:"PtrI16" form:"PtrI16"`
	I32         int32    `json:"I32" form:"I32"`
	PtrI32      *int32   `json:"PtrI32" form:"PtrI32"`
	I64         int64    `json:"I64" form:"I64"`
	PtrI64      *int64   `json:"PtrI64" form:"PtrI64"`
	UI          uint     `json:"UI" form:"UI"`
	PtrUI       *uint    `json:"PtrUI" form:"PtrUI"`
	UI8         uint8    `json:"UI8" form:"UI8"`
	PtrUI8      *uint8   `json:"PtrUI8" form:"PtrUI8"`
	UI16        uint16   `json:"UI16" form:"UI16"`
	PtrUI16     *uint16  `json:"PtrUI16" form:"PtrUI16"`
	UI32        uint32   `json:"UI32" form:"UI32"`
	PtrUI32     *uint32  `json:"PtrUI32" form:"PtrUI32"`
	UI64        uint64   `json:"UI64" form:"UI64"`
	PtrUI64     *uint64  `json:"PtrUI64" form:"PtrUI64"`
	B           bool     `json:"B" form:"B"`
	PtrB        *bool    `json:"PtrB" form:"PtrB"`
	F32         float32  `json:"F32" form:"F32"`
	PtrF32      *float32 `json:"PtrF32" form:"PtrF32"`
	F64         float64  `json:"F64" form:"F64"`
	PtrF64      *float64 `json:"PtrF64" form:"PtrF64"`
	S           string   `json:"S" form:"S"`
	PtrS        *string  `json:"PtrS" form:"PtrS"`
	cantSet     string
	DoesntExist string      `json:"DoesntExist" form:"DoesntExist"`
	GoT         time.Time   `json:"GoT" form:"GoT"`
	GoTptr      *time.Time  `json:"GoTptr" form:"GoTptr"`
	T           Timestamp   `json:"T" form:"T"`
	Tptr        *Timestamp  `json:"Tptr" form:"Tptr"`
	SA          StringArray `json:"SA" form:"SA"`
}

type Timestamp time.Time
type TA []Timestamp
type StringArray []string
type Struct struct {
	Foo string
}
type Bar struct {
	Baz int `json:"baz" query:"baz"`
}

func (t *Timestamp) UnmarshalParam(src string) error {
	ts, err := time.Parse(time.RFC3339, src)
	*t = Timestamp(ts)
	return err
}

func (a *StringArray) UnmarshalParam(src string) error {
	*a = StringArray(strings.Split(src, ","))
	return nil
}

func (s *Struct) UnmarshalParam(src string) error {
	*s = Struct{
		Foo: src,
	}
	return nil
}

func (t bindTestStruct) GetCantSet() string {
	return t.cantSet
}

var values = map[string][]string{
	"I":       {"0"},
	"PtrI":    {"0"},
	"I8":      {"8"},
	"PtrI8":   {"8"},
	"I16":     {"16"},
	"PtrI16":  {"16"},
	"I32":     {"32"},
	"PtrI32":  {"32"},
	"I64":     {"64"},
	"PtrI64":  {"64"},
	"UI":      {"0"},
	"PtrUI":   {"0"},
	"UI8":     {"8"},
	"PtrUI8":  {"8"},
	"UI16":    {"16"},
	"PtrUI16": {"16"},
	"UI32":    {"32"},
	"PtrUI32": {"32"},
	"UI64":    {"64"},
	"PtrUI64": {"64"},
	"B":       {"true"},
	"PtrB":    {"true"},
	"F32":     {"32.5"},
	"PtrF32":  {"32.5"},
	"F64":     {"64.5"},
	"PtrF64":  {"64.5"},
	"S":       {"test"},
	"PtrS":    {"test"},
	"cantSet": {"test"},
	"T":       {"2016-12-06T19:09:05+01:00"},
	"Tptr":    {"2016-12-06T19:09:05+01:00"},
	"GoT":     {"2016-12-06T19:09:05+01:00"},
	"GoTptr":  {"2016-12-06T19:09:05+01:00"},
	"ST":      {"bar"},
}

// ptr return pointer to value. This is useful as `v := []*int8{&int8(1)}` will not compile
func ptr[T any](value T) *T {
	return &value
}

func TestToMultipleFields(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?id=1&ID=2", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	type Root struct {
		ID     int64 `query:"id"`
		Child2 struct {
			ID int64
		}
		Child1 struct {
			ID int64 `query:"id"`
		}
	}

	u := new(Root)
	err := c.Bind(u)
	if assert.NoError(t, err) {
		assert.Equal(t, int64(1), u.ID)        // perfectly reasonable
		assert.Equal(t, int64(1), u.Child1.ID) // untagged struct containing tagged field gets filled (by tag)
		assert.Equal(t, int64(0), u.Child2.ID) // untagged struct containing untagged field should not be bind
	}
}

func TestBindJSON(t *testing.T) {
	testBindOkay(t, strings.NewReader(userJSON), nil, MIMEApplicationJSON)
	testBindOkay(t, strings.NewReader(userJSON), dummyQuery, MIMEApplicationJSON)
	testBindArrayOkay(t, strings.NewReader(usersJSON), nil, MIMEApplicationJSON)
	testBindArrayOkay(t, strings.NewReader(usersJSON), dummyQuery, MIMEApplicationJSON)
	testBindError(t, strings.NewReader(invalidContent), MIMEApplicationJSON, &json.SyntaxError{})
	testBindError(t, strings.NewReader(userJSONInvalidType), MIMEApplicationJSON, &json.UnmarshalTypeError{})
}

func TestBindXML(t *testing.T) {
	testBindOkay(t, strings.NewReader(userXML), nil, MIMEApplicationXML)
	testBindOkay(t, strings.NewReader(userXML), dummyQuery, MIMEApplicationXML)
	testBindArrayOkay(t, strings.NewReader(userXML), nil, MIMEApplicationXML)
	testBindArrayOkay(t, strings.NewReader(userXML), dummyQuery, MIMEApplicationXML)
	testBindError(t, strings.NewReader(invalidContent), MIMEApplicationXML, errors.New(""))
	testBindError(t, strings.NewReader(userXMLConvertNumberError), MIMEApplicationXML, &strconv.NumError{})
	testBindError(t, strings.NewReader(userXMLUnsupportedTypeError), MIMEApplicationXML, &xml.SyntaxError{})
	testBindOkay(t, strings.NewReader(userXML), nil, MIMETextXML)
	testBindOkay(t, strings.NewReader(userXML), dummyQuery, MIMETextXML)
	testBindError(t, strings.NewReader(invalidContent), MIMETextXML, errors.New(""))
	testBindError(t, strings.NewReader(userXMLConvertNumberError), MIMETextXML, &strconv.NumError{})
	testBindError(t, strings.NewReader(userXMLUnsupportedTypeError), MIMETextXML, &xml.SyntaxError{})
}

func TestBindForm(t *testing.T) {

	testBindOkay(t, strings.NewReader(userForm), nil, MIMEApplicationForm)
	testBindOkay(t, strings.NewReader(userForm), dummyQuery, MIMEApplicationForm)
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userForm))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	req.Header.Set(HeaderContentType, MIMEApplicationForm)
	err := c.Bind(&[]struct{ Field string }{})
	assert.Error(t, err)
}

func TestBindQueryParams(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?id=1&name=Jon+Snow", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	u := new(user)
	err := c.Bind(u)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, u.ID)
		assert.Equal(t, "Jon Snow", u.Name)
	}
}

func TestBindQueryParamsCaseInsensitive(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?ID=1&NAME=Jon+Snow", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	u := new(user)
	err := c.Bind(u)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, u.ID)
		assert.Equal(t, "Jon Snow", u.Name)
	}
}

func TestBindQueryParamsCaseSensitivePrioritized(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?id=1&ID=2&NAME=Jon+Snow&name=Jon+Doe", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	u := new(user)
	err := c.Bind(u)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, u.ID)
		assert.Equal(t, "Jon Doe", u.Name)
	}
}

func TestBindHeaderParam(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Name", "Jon Doe")
	req.Header.Set("Id", "2")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	u := new(user)
	err := (&DefaultBinder{}).BindHeaders(c, u)
	if assert.NoError(t, err) {
		assert.Equal(t, 2, u.ID)
		assert.Equal(t, "Jon Doe", u.Name)
	}
}

func TestBindHeaderParamBadType(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Id", "salamander")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	u := new(user)
	err := (&DefaultBinder{}).BindHeaders(c, u)
	assert.Error(t, err)

	httpErr, ok := err.(*HTTPError)
	if assert.True(t, ok) {
		assert.Equal(t, http.StatusBadRequest, httpErr.Code)
	}
}

func TestBindUnmarshalParam(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?ts=2016-12-06T19:09:05Z&sa=one,two,three&ta=2016-12-06T19:09:05Z&ta=2016-12-06T19:09:05Z&ST=baz", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	result := struct {
		T         Timestamp   `query:"ts"`
		TA        []Timestamp `query:"ta"`
		SA        StringArray `query:"sa"`
		ST        Struct
		StWithTag struct {
			Foo string `query:"st"`
		}
	}{}
	err := c.Bind(&result)
	ts := Timestamp(time.Date(2016, 12, 6, 19, 9, 5, 0, time.UTC))

	if assert.NoError(t, err) {
		//		assert.Equal( Timestamp(reflect.TypeOf(&Timestamp{}), time.Date(2016, 12, 6, 19, 9, 5, 0, time.UTC)), result.T)
		assert.Equal(t, ts, result.T)
		assert.Equal(t, StringArray([]string{"one", "two", "three"}), result.SA)
		assert.Equal(t, []Timestamp{ts, ts}, result.TA)
		assert.Equal(t, Struct{""}, result.ST)       // child struct does not have a field with matching tag
		assert.Equal(t, "baz", result.StWithTag.Foo) // child struct has field with matching tag
	}
}

func TestBindUnmarshalText(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?ts=2016-12-06T19:09:05Z&sa=one,two,three&ta=2016-12-06T19:09:05Z&ta=2016-12-06T19:09:05Z&ST=baz", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	result := struct {
		T  time.Time   `query:"ts"`
		TA []time.Time `query:"ta"`
		SA StringArray `query:"sa"`
		ST Struct
	}{}
	err := c.Bind(&result)
	ts := time.Date(2016, 12, 6, 19, 9, 5, 0, time.UTC)
	if assert.NoError(t, err) {
		//		assert.Equal(t, Timestamp(reflect.TypeOf(&Timestamp{}), time.Date(2016, 12, 6, 19, 9, 5, 0, time.UTC)), result.T)
		assert.Equal(t, ts, result.T)
		assert.Equal(t, StringArray([]string{"one", "two", "three"}), result.SA)
		assert.Equal(t, []time.Time{ts, ts}, result.TA)
		assert.Equal(t, Struct{""}, result.ST) // field in child struct does not have tag
	}
}

func TestBindUnmarshalParamPtr(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?ts=2016-12-06T19:09:05Z", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	result := struct {
		Tptr *Timestamp `query:"ts"`
	}{}
	err := c.Bind(&result)
	if assert.NoError(t, err) {
		assert.Equal(t, Timestamp(time.Date(2016, 12, 6, 19, 9, 5, 0, time.UTC)), *result.Tptr)
	}
}

func TestBindUnmarshalParamAnonymousFieldPtr(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?baz=1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	result := struct {
		*Bar
	}{&Bar{}}
	err := c.Bind(&result)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, result.Baz)
	}
}

func TestBindUnmarshalParamAnonymousFieldPtrNil(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?baz=1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	result := struct {
		*Bar
	}{}
	err := c.Bind(&result)
	if assert.NoError(t, err) {
		assert.Nil(t, result.Bar)
	}
}

func TestBindUnmarshalParamAnonymousFieldPtrCustomTag(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, `/?bar={"baz":100}&baz=1`, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	result := struct {
		*Bar `json:"bar" query:"bar"`
	}{&Bar{}}
	err := c.Bind(&result)
	assert.Contains(t, err.Error(), "query/param/form tags are not allowed with anonymous struct field")
}

func TestBindUnmarshalTextPtr(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/?ts=2016-12-06T19:09:05Z", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	result := struct {
		Tptr *time.Time `query:"ts"`
	}{}
	err := c.Bind(&result)
	if assert.NoError(t, err) {
		assert.Equal(t, time.Date(2016, 12, 6, 19, 9, 5, 0, time.UTC), *result.Tptr)
	}
}

func TestBindMultipartForm(t *testing.T) {
	bodyBuffer := new(bytes.Buffer)
	mw := multipart.NewWriter(bodyBuffer)
	mw.WriteField("id", "1")
	mw.WriteField("name", "Jon Snow")
	mw.Close()
	body := bodyBuffer.Bytes()

	testBindOkay(t, bytes.NewReader(body), nil, mw.FormDataContentType())
	testBindOkay(t, bytes.NewReader(body), dummyQuery, mw.FormDataContentType())
}

func TestBindUnsupportedMediaType(t *testing.T) {
	testBindError(t, strings.NewReader(invalidContent), MIMEApplicationJSON, &json.SyntaxError{})
}

func TestDefaultBinder_bindDataToMap(t *testing.T) {
	exampleData := map[string][]string{
		"multiple": {"1", "2"},
		"single":   {"3"},
	}

	t.Run("ok, bind to map[string]string", func(t *testing.T) {
		dest := map[string]string{}
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t,
			map[string]string{
				"multiple": "1",
				"single":   "3",
			},
			dest,
		)
	})

	t.Run("ok, bind to map[string]string with nil map", func(t *testing.T) {
		var dest map[string]string
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t,
			map[string]string{
				"multiple": "1",
				"single":   "3",
			},
			dest,
		)
	})

	t.Run("ok, bind to map[string][]string", func(t *testing.T) {
		dest := map[string][]string{}
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t,
			map[string][]string{
				"multiple": {"1", "2"},
				"single":   {"3"},
			},
			dest,
		)
	})

	t.Run("ok, bind to map[string][]string with nil map", func(t *testing.T) {
		var dest map[string][]string
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t,
			map[string][]string{
				"multiple": {"1", "2"},
				"single":   {"3"},
			},
			dest,
		)
	})

	t.Run("ok, bind to map[string]interface", func(t *testing.T) {
		dest := map[string]interface{}{}
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t,
			map[string]interface{}{
				"multiple": "1",
				"single":   "3",
			},
			dest,
		)
	})

	t.Run("ok, bind to map[string]interface with nil map", func(t *testing.T) {
		var dest map[string]interface{}
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t,
			map[string]interface{}{
				"multiple": "1",
				"single":   "3",
			},
			dest,
		)
	})

	t.Run("ok, bind to map[string]int skips", func(t *testing.T) {
		dest := map[string]int{}
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t, map[string]int{}, dest)
	})

	t.Run("ok, bind to map[string]int skips with nil map", func(t *testing.T) {
		var dest map[string]int
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t, map[string]int(nil), dest)
	})

	t.Run("ok, bind to map[string][]int skips", func(t *testing.T) {
		dest := map[string][]int{}
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t, map[string][]int{}, dest)
	})

	t.Run("ok, bind to map[string][]int skips with nil map", func(t *testing.T) {
		var dest map[string][]int
		assert.NoError(t, new(DefaultBinder).bindData(&dest, exampleData, "param", nil))
		assert.Equal(t, map[string][]int(nil), dest)
	})
}

func TestBindbindData(t *testing.T) {
	ts := new(bindTestStruct)
	b := new(DefaultBinder)
	err := b.bindData(ts, values, "form", nil)
	assert.NoError(t, err)

	assert.Equal(t, 0, ts.I)
	assert.Equal(t, int8(0), ts.I8)
	assert.Equal(t, int16(0), ts.I16)
	assert.Equal(t, int32(0), ts.I32)
	assert.Equal(t, int64(0), ts.I64)
	assert.Equal(t, uint(0), ts.UI)
	assert.Equal(t, uint8(0), ts.UI8)
	assert.Equal(t, uint16(0), ts.UI16)
	assert.Equal(t, uint32(0), ts.UI32)
	assert.Equal(t, uint64(0), ts.UI64)
	assert.Equal(t, false, ts.B)
	assert.Equal(t, float32(0), ts.F32)
	assert.Equal(t, float64(0), ts.F64)
	assert.Equal(t, "", ts.S)
	assert.Equal(t, "", ts.cantSet)
}

func TestBindParam(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/users/:id/:name")
	c.SetParamNames("id", "name")
	c.SetParamValues("1", "Jon Snow")

	u := new(user)
	err := c.Bind(u)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, u.ID)
		assert.Equal(t, "Jon Snow", u.Name)
	}

	// Second test for the absence of a param
	c2 := e.NewContext(req, rec)
	c2.SetPath("/users/:id")
	c2.SetParamNames("id")
	c2.SetParamValues("1")

	u = new(user)
	err = c2.Bind(u)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, u.ID)
		assert.Equal(t, "", u.Name)
	}

	// Bind something with param and post data payload
	body := bytes.NewBufferString(`{ "name": "Jon Snow" }`)
	e2 := New()
	req2 := httptest.NewRequest(http.MethodPost, "/", body)
	req2.Header.Set(HeaderContentType, MIMEApplicationJSON)

	rec2 := httptest.NewRecorder()

	c3 := e2.NewContext(req2, rec2)
	c3.SetPath("/users/:id")
	c3.SetParamNames("id")
	c3.SetParamValues("1")

	u = new(user)
	err = c3.Bind(u)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, u.ID)
		assert.Equal(t, "Jon Snow", u.Name)
	}
}

func TestBindUnmarshalTypeError(t *testing.T) {
	body := bytes.NewBufferString(`{ "id": "text" }`)
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.Header.Set(HeaderContentType, MIMEApplicationJSON)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	u := new(user)

	err := c.Bind(u)

	he := &HTTPError{Code: http.StatusBadRequest, Message: "Unmarshal type error: expected=int, got=string, field=id, offset=14", Internal: err.(*HTTPError).Internal}

	assert.Equal(t, he, err)
}

func TestBindSetWithProperType(t *testing.T) {
	ts := new(bindTestStruct)
	typ := reflect.TypeOf(ts).Elem()
	val := reflect.ValueOf(ts).Elem()
	for i := 0; i < typ.NumField(); i++ {
		typeField := typ.Field(i)
		structField := val.Field(i)
		if !structField.CanSet() {
			continue
		}
		if len(values[typeField.Name]) == 0 {
			continue
		}
		val := values[typeField.Name][0]
		err := setWithProperType(typeField.Type.Kind(), val, structField)
		assert.NoError(t, err)
	}
	assertBindTestStruct(t, ts)

	type foo struct {
		Bar bytes.Buffer
	}
	v := &foo{}
	typ = reflect.TypeOf(v).Elem()
	val = reflect.ValueOf(v).Elem()
	assert.Error(t, setWithProperType(typ.Field(0).Type.Kind(), "5", val.Field(0)))
}

func BenchmarkBindbindDataWithTags(b *testing.B) {
	b.ReportAllocs()
	ts := new(bindTestStructWithTags)
	binder := new(DefaultBinder)
	var err error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = binder.bindData(ts, values, "form", nil)
	}
	assert.NoError(b, err)
	assertBindTestStruct(b, (*bindTestStruct)(ts))
}

func assertBindTestStruct(tb testing.TB, ts *bindTestStruct) {
	assert.Equal(tb, 0, ts.I)
	assert.Equal(tb, int8(8), ts.I8)
	assert.Equal(tb, int16(16), ts.I16)
	assert.Equal(tb, int32(32), ts.I32)
	assert.Equal(tb, int64(64), ts.I64)
	assert.Equal(tb, uint(0), ts.UI)
	assert.Equal(tb, uint8(8), ts.UI8)
	assert.Equal(tb, uint16(16), ts.UI16)
	assert.Equal(tb, uint32(32), ts.UI32)
	assert.Equal(tb, uint64(64), ts.UI64)
	assert.Equal(tb, true, ts.B)
	assert.Equal(tb, float32(32.5), ts.F32)
	assert.Equal(tb, float64(64.5), ts.F64)
	assert.Equal(tb, "test", ts.S)
	assert.Equal(tb, "", ts.GetCantSet())
}

func testBindOkay(t *testing.T, r io.Reader, query url.Values, ctype string) {
	e := New()
	path := "/"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req := httptest.NewRequest(http.MethodPost, path, r)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	req.Header.Set(HeaderContentType, ctype)
	u := new(user)
	err := c.Bind(u)
	if assert.Equal(t, nil, err) {
		assert.Equal(t, 1, u.ID)
		assert.Equal(t, "Jon Snow", u.Name)
	}
}

func testBindArrayOkay(t *testing.T, r io.Reader, query url.Values, ctype string) {
	e := New()
	path := "/"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req := httptest.NewRequest(http.MethodPost, path, r)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	req.Header.Set(HeaderContentType, ctype)
	u := []user{}
	err := c.Bind(&u)
	if assert.NoError(t, err) {
		assert.Equal(t, 1, len(u))
		assert.Equal(t, 1, u[0].ID)
		assert.Equal(t, "Jon Snow", u[0].Name)
	}
}

func testBindError(t *testing.T, r io.Reader, ctype string, expectedInternal error) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", r)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	req.Header.Set(HeaderContentType, ctype)
	u := new(user)
	err := c.Bind(u)

	switch {
	case strings.HasPrefix(ctype, MIMEApplicationJSON), strings.HasPrefix(ctype, MIMEApplicationXML), strings.HasPrefix(ctype, MIMETextXML),
		strings.HasPrefix(ctype, MIMEApplicationForm), strings.HasPrefix(ctype, MIMEMultipartForm):
		if assert.IsType(t, new(HTTPError), err) {
			assert.Equal(t, http.StatusBadRequest, err.(*HTTPError).Code)
			assert.IsType(t, expectedInternal, err.(*HTTPError).Internal)
		}
	default:
		if assert.IsType(t, new(HTTPError), err) {
			assert.Equal(t, ErrUnsupportedMediaType, err)
			assert.IsType(t, expectedInternal, err.(*HTTPError).Internal)
		}
	}
}

func TestDefaultBinder_BindToStructFromMixedSources(t *testing.T) {
	// tests to check binding behaviour when multiple sources (path params, query params and request body) are in use
	// binding is done in steps and one source could overwrite previous source binded data
	// these tests are to document this behaviour and detect further possible regressions when bind implementation is changed

	type Opts struct {
		ID   int    `json:"id" form:"id" query:"id"`
		Node string `json:"node" form:"node" query:"node" param:"node"`
		Lang string
	}

	var testCases = []struct {
		name             string
		givenURL         string
		givenContent     io.Reader
		givenMethod      string
		whenBindTarget   interface{}
		whenNoPathParams bool
		expect           interface{}
		expectError      string
	}{
		{
			name:         "ok, POST bind to struct with: path param + query param + body",
			givenMethod:  http.MethodPost,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`{"id": 1}`),
			expect:       &Opts{ID: 1, Node: "node_from_path"}, // query params are not used, node is filled from path
		},
		{
			name:         "ok, PUT bind to struct with: path param + query param + body",
			givenMethod:  http.MethodPut,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`{"id": 1}`),
			expect:       &Opts{ID: 1, Node: "node_from_path"}, // query params are not used
		},
		{
			name:         "ok, GET bind to struct with: path param + query param + body",
			givenMethod:  http.MethodGet,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`{"id": 1}`),
			expect:       &Opts{ID: 1, Node: "xxx"}, // query overwrites previous path value
		},
		{
			name:         "ok, GET bind to struct with: path param + query param + body",
			givenMethod:  http.MethodGet,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`{"id": 1, "node": "zzz"}`),
			expect:       &Opts{ID: 1, Node: "zzz"}, // body is binded last and overwrites previous (path,query) values
		},
		{
			name:         "ok, DELETE bind to struct with: path param + query param + body",
			givenMethod:  http.MethodDelete,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`{"id": 1, "node": "zzz"}`),
			expect:       &Opts{ID: 1, Node: "zzz"}, // for DELETE body is binded after query params
		},
		{
			name:         "ok, POST bind to struct with: path param + body",
			givenMethod:  http.MethodPost,
			givenURL:     "/api/real_node/endpoint",
			givenContent: strings.NewReader(`{"id": 1}`),
			expect:       &Opts{ID: 1, Node: "node_from_path"},
		},
		{
			name:         "ok, POST bind to struct with path + query + body = body has priority",
			givenMethod:  http.MethodPost,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`{"id": 1, "node": "zzz"}`),
			expect:       &Opts{ID: 1, Node: "zzz"}, // field value from content has higher priority
		},
		{
			name:         "nok, POST body bind failure",
			givenMethod:  http.MethodPost,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`{`),
			expect:       &Opts{ID: 0, Node: "node_from_path"}, // query binding has already modified bind target
			expectError:  "code=400, message=unexpected EOF, internal=unexpected EOF",
		},
		{
			name:         "nok, GET with body bind failure when types are not convertible",
			givenMethod:  http.MethodGet,
			givenURL:     "/api/real_node/endpoint?id=nope",
			givenContent: strings.NewReader(`{"id": 1, "node": "zzz"}`),
			expect:       &Opts{ID: 0, Node: "node_from_path"}, // path params binding has already modified bind target
			expectError:  "code=400, message=strconv.ParseInt: parsing \"nope\": invalid syntax, internal=strconv.ParseInt: parsing \"nope\": invalid syntax",
		},
		{
			name:         "nok, GET body bind failure - trying to bind json array to struct",
			givenMethod:  http.MethodGet,
			givenURL:     "/api/real_node/endpoint?node=xxx",
			givenContent: strings.NewReader(`[{"id": 1}]`),
			expect:       &Opts{ID: 0, Node: "xxx"}, // query binding has already modified bind target
			expectError:  "code=400, message=Unmarshal type error: expected=echo.Opts, got=array, field=, offset=1, internal=json: cannot unmarshal array into Go value of type echo.Opts",
		},
		{ // query param is ignored as we do not know where exactly to bind it in slice
			name:             "ok, GET bind to struct slice, ignore query param",
			givenMethod:      http.MethodGet,
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenContent:     strings.NewReader(`[{"id": 1}]`),
			whenNoPathParams: true,
			whenBindTarget:   &[]Opts{},
			expect: &[]Opts{
				{ID: 1, Node: ""},
			},
		},
		{ // binding query params interferes with body. b.BindBody() should be used to bind only body to slice
			name:             "ok, POST binding to slice should not be affected query params types",
			givenMethod:      http.MethodPost,
			givenURL:         "/api/real_node/endpoint?id=nope&node=xxx",
			givenContent:     strings.NewReader(`[{"id": 1}]`),
			whenNoPathParams: true,
			whenBindTarget:   &[]Opts{},
			expect:           &[]Opts{{ID: 1}},
			expectError:      "",
		},
		{ // path param is ignored as we do not know where exactly to bind it in slice
			name:           "ok, GET bind to struct slice, ignore path param",
			givenMethod:    http.MethodGet,
			givenURL:       "/api/real_node/endpoint?node=xxx",
			givenContent:   strings.NewReader(`[{"id": 1}]`),
			whenBindTarget: &[]Opts{},
			expect: &[]Opts{
				{ID: 1, Node: ""},
			},
		},
		{
			name:             "ok, GET body bind json array to slice",
			givenMethod:      http.MethodGet,
			givenURL:         "/api/real_node/endpoint",
			givenContent:     strings.NewReader(`[{"id": 1}]`),
			whenNoPathParams: true,
			whenBindTarget:   &[]Opts{},
			expect:           &[]Opts{{ID: 1, Node: ""}},
			expectError:      "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			// assume route we are testing is "/api/:node/endpoint?some_query_params=here"
			req := httptest.NewRequest(tc.givenMethod, tc.givenURL, tc.givenContent)
			req.Header.Set(HeaderContentType, MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if !tc.whenNoPathParams {
				c.SetParamNames("node")
				c.SetParamValues("node_from_path")
			}

			var bindTarget interface{}
			if tc.whenBindTarget != nil {
				bindTarget = tc.whenBindTarget
			} else {
				bindTarget = &Opts{}
			}
			b := new(DefaultBinder)

			err := b.Bind(bindTarget, c)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, bindTarget)
		})
	}
}

func TestDefaultBinder_BindBody(t *testing.T) {
	// tests to check binding behaviour when multiple sources (path params, query params and request body) are in use
	// generally when binding from request body - URL and path params are ignored - unless form is being binded.
	// these tests are to document this behaviour and detect further possible regressions when bind implementation is changed

	type Node struct {
		ID   int    `json:"id" xml:"id" form:"id" query:"id"`
		Node string `json:"node" xml:"node" form:"node" query:"node" param:"node"`
	}
	type Nodes struct {
		Nodes []Node `xml:"node" form:"node"`
	}

	var testCases = []struct {
		name             string
		givenURL         string
		givenContent     io.Reader
		givenMethod      string
		givenContentType string
		whenNoPathParams bool
		whenChunkedBody  bool
		whenBindTarget   interface{}
		expect           interface{}
		expectError      string
	}{
		{
			name:             "ok, JSON POST bind to struct with: path + query + empty field in body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationJSON,
			givenContent:     strings.NewReader(`{"id": 1}`),
			expect:           &Node{ID: 1, Node: ""}, // path params or query params should not interfere with body
		},
		{
			name:             "ok, JSON POST bind to struct with: path + query + body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationJSON,
			givenContent:     strings.NewReader(`{"id": 1, "node": "zzz"}`),
			expect:           &Node{ID: 1, Node: "zzz"}, // field value from content has higher priority
		},
		{
			name:             "ok, JSON POST body bind json array to slice (has matching path/query params)",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationJSON,
			givenContent:     strings.NewReader(`[{"id": 1}]`),
			whenNoPathParams: true,
			whenBindTarget:   &[]Node{},
			expect:           &[]Node{{ID: 1, Node: ""}},
			expectError:      "",
		},
		{ // rare case as GET is not usually used to send request body
			name:             "ok, JSON GET bind to struct with: path + query + empty field in body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodGet,
			givenContentType: MIMEApplicationJSON,
			givenContent:     strings.NewReader(`{"id": 1}`),
			expect:           &Node{ID: 1, Node: ""}, // path params or query params should not interfere with body
		},
		{ // rare case as GET is not usually used to send request body
			name:             "ok, JSON GET bind to struct with: path + query + body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodGet,
			givenContentType: MIMEApplicationJSON,
			givenContent:     strings.NewReader(`{"id": 1, "node": "zzz"}`),
			expect:           &Node{ID: 1, Node: "zzz"}, // field value from content has higher priority
		},
		{
			name:             "nok, JSON POST body bind failure",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationJSON,
			givenContent:     strings.NewReader(`{`),
			expect:           &Node{ID: 0, Node: ""},
			expectError:      "code=400, message=unexpected EOF, internal=unexpected EOF",
		},
		{
			name:             "ok, XML POST bind to struct with: path + query + empty body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationXML,
			givenContent:     strings.NewReader(`<node><id>1</id><node>yyy</node></node>`),
			expect:           &Node{ID: 1, Node: "yyy"},
		},
		{
			name:             "ok, XML POST bind array to slice with: path + query + body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationXML,
			givenContent:     strings.NewReader(`<nodes><node><id>1</id><node>yyy</node></node></nodes>`),
			whenBindTarget:   &Nodes{},
			expect:           &Nodes{Nodes: []Node{{ID: 1, Node: "yyy"}}},
		},
		{
			name:             "nok, XML POST bind failure",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationXML,
			givenContent:     strings.NewReader(`<node><`),
			expect:           &Node{ID: 0, Node: ""},
			expectError:      "code=400, message=Syntax error: line=1, error=XML syntax error on line 1: unexpected EOF, internal=XML syntax error on line 1: unexpected EOF",
		},
		{
			name:             "ok, FORM POST bind to struct with: path + query + body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationForm,
			givenContent:     strings.NewReader(`id=1&node=yyy`),
			expect:           &Node{ID: 1, Node: "yyy"},
		},
		{
			// NB: form values are taken from BOTH body and query for POST/PUT/PATCH by standard library implementation
			// See: https://golang.org/pkg/net/http/#Request.ParseForm
			name:             "ok, FORM POST bind to struct with: path + query + empty field in body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationForm,
			givenContent:     strings.NewReader(`id=1`),
			expect:           &Node{ID: 1, Node: "xxx"},
		},
		{
			// NB: form values are taken from query by standard library implementation
			// See: https://golang.org/pkg/net/http/#Request.ParseForm
			name:             "ok, FORM GET bind to struct with: path + query + empty field in body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodGet,
			givenContentType: MIMEApplicationForm,
			givenContent:     strings.NewReader(`id=1`),
			expect:           &Node{ID: 0, Node: "xxx"}, // 'xxx' is taken from URL and body is not used with GET by implementation
		},
		{
			name:             "nok, unsupported content type",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMETextPlain,
			givenContent:     strings.NewReader(`<html></html>`),
			expect:           &Node{ID: 0, Node: ""},
			expectError:      "code=415, message=Unsupported Media Type",
		},
		// FIXME: REASON in Go 1.24 and earlier http.NoBody would result ContentLength=-1
		// 		but as of Go 1.25 http.NoBody would result ContentLength=0
		//		I am too lazy to bother documenting this as 2 version specific tests.
		//{
		//	name:             "nok, JSON POST with http.NoBody",
		//	givenURL:         "/api/real_node/endpoint?node=xxx",
		//	givenMethod:      http.MethodPost,
		//	givenContentType: MIMEApplicationJSON,
		//	givenContent:     http.NoBody,
		//	expect:           &Node{ID: 0, Node: ""},
		//	expectError:      "code=400, message=EOF, internal=EOF",
		//},
		{
			name:             "ok, JSON POST with empty body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationJSON,
			givenContent:     strings.NewReader(""),
			expect:           &Node{ID: 0, Node: ""},
		},
		{
			name:             "ok, JSON POST bind to struct with: path + query + chunked body",
			givenURL:         "/api/real_node/endpoint?node=xxx",
			givenMethod:      http.MethodPost,
			givenContentType: MIMEApplicationJSON,
			givenContent:     httputil.NewChunkedReader(strings.NewReader("18\r\n" + `{"id": 1, "node": "zzz"}` + "\r\n0\r\n")),
			whenChunkedBody:  true,
			expect:           &Node{ID: 1, Node: "zzz"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			// assume route we are testing is "/api/:node/endpoint?some_query_params=here"
			req := httptest.NewRequest(tc.givenMethod, tc.givenURL, tc.givenContent)
			switch tc.givenContentType {
			case MIMEApplicationXML:
				req.Header.Set(HeaderContentType, MIMEApplicationXML)
			case MIMEApplicationForm:
				req.Header.Set(HeaderContentType, MIMEApplicationForm)
			case MIMEApplicationJSON:
				req.Header.Set(HeaderContentType, MIMEApplicationJSON)
			}
			if tc.whenChunkedBody {
				req.ContentLength = -1
				req.TransferEncoding = append(req.TransferEncoding, "chunked")
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if !tc.whenNoPathParams {
				c.SetParamNames("node")
				c.SetParamValues("real_node")
			}

			var bindTarget interface{}
			if tc.whenBindTarget != nil {
				bindTarget = tc.whenBindTarget
			} else {
				bindTarget = &Node{}
			}
			b := new(DefaultBinder)

			err := b.BindBody(c, bindTarget)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expect, bindTarget)
		})
	}
}

func testBindURL(queryString string, target any) error {
	e := New()
	req := httptest.NewRequest(http.MethodGet, queryString, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c.Bind(target)
}

type unixTimestamp struct {
	Time time.Time
}

func (t *unixTimestamp) UnmarshalParam(param string) error {
	n, err := strconv.ParseInt(param, 10, 64)
	if err != nil {
		return fmt.Errorf("'%s' is not an integer", param)
	}
	*t = unixTimestamp{Time: time.Unix(n, 0)}
	return err
}

type IntArrayA []int

// UnmarshalParam converts value to *Int64Slice.  This allows the API to accept
// a comma-separated list of integers as a query parameter.
func (i *IntArrayA) UnmarshalParam(value string) error {
	var values = strings.Split(value, ",")
	var numbers = make([]int, 0, len(values))

	for _, v := range values {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("'%s' is not an integer", v)
		}

		numbers = append(numbers, int(n))
	}

	*i = append(*i, numbers...)
	return nil
}

func TestBindUnmarshalParamExtras(t *testing.T) {
	// this test documents how bind handles `BindUnmarshaler` interface:
	// NOTE: BindUnmarshaler chooses first input value to be bound.

	t.Run("nok, unmarshalling fails", func(t *testing.T) {
		result := struct {
			V unixTimestamp `query:"t"`
		}{}
		err := testBindURL("/?t=xxxx", &result)

		assert.EqualError(t, err, "code=400, message='xxxx' is not an integer, internal='xxxx' is not an integer")
	})

	t.Run("ok, target is struct", func(t *testing.T) {
		result := struct {
			V unixTimestamp `query:"t"`
		}{}
		err := testBindURL("/?t=1710095540&t=1710095541", &result)

		assert.NoError(t, err)
		expect := unixTimestamp{
			Time: time.Unix(1710095540, 0),
		}
		assert.Equal(t, expect, result.V)
	})

	t.Run("ok, target is an alias to slice and is nil, append only values from first", func(t *testing.T) {
		result := struct {
			V IntArrayA `query:"a"`
		}{}
		err := testBindURL("/?a=1,2,3&a=4,5,6", &result)

		assert.NoError(t, err)
		assert.Equal(t, IntArrayA([]int{1, 2, 3}), result.V)
	})

	t.Run("ok, target is an alias to slice and is nil, single input", func(t *testing.T) {
		result := struct {
			V IntArrayA `query:"a"`
		}{}
		err := testBindURL("/?a=1,2", &result)

		assert.NoError(t, err)
		assert.Equal(t, IntArrayA([]int{1, 2}), result.V)
	})

	t.Run("ok, target is pointer an alias to slice and is nil", func(t *testing.T) {
		result := struct {
			V *IntArrayA `query:"a"`
		}{}
		err := testBindURL("/?a=1&a=4,5,6", &result)

		assert.NoError(t, err)
		var expected = IntArrayA([]int{1})
		assert.Equal(t, &expected, result.V)
	})

	t.Run("ok, target is pointer an alias to slice and is NOT nil", func(t *testing.T) {
		result := struct {
			V *IntArrayA `query:"a"`
		}{}
		result.V = new(IntArrayA) // NOT nil

		err := testBindURL("/?a=1&a=4,5,6", &result)

		assert.NoError(t, err)
		var expected = IntArrayA([]int{1})
		assert.Equal(t, &expected, result.V)
	})
}

type unixTimestampLast struct {
	Time time.Time
}

// this is silly example for `bindMultipleUnmarshaler` for type that uses last input value for unmarshalling
func (t *unixTimestampLast) UnmarshalParams(params []string) error {
	lastInput := params[len(params)-1]
	n, err := strconv.ParseInt(lastInput, 10, 64)
	if err != nil {
		return fmt.Errorf("'%s' is not an integer", lastInput)
	}
	*t = unixTimestampLast{Time: time.Unix(n, 0)}
	return err
}

type IntArrayB []int

func (i *IntArrayB) UnmarshalParams(params []string) error {
	var numbers = make([]int, 0, len(params))

	for _, param := range params {
		var values = strings.Split(param, ",")
		for _, v := range values {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("'%s' is not an integer", v)
			}
			numbers = append(numbers, int(n))
		}
	}

	*i = append(*i, numbers...)
	return nil
}

func TestBindUnmarshalParams(t *testing.T) {
	// this test documents how bind handles `bindMultipleUnmarshaler` interface:

	t.Run("nok, unmarshalling fails", func(t *testing.T) {
		result := struct {
			V unixTimestampLast `query:"t"`
		}{}
		err := testBindURL("/?t=xxxx", &result)

		assert.EqualError(t, err, "code=400, message='xxxx' is not an integer, internal='xxxx' is not an integer")
	})

	t.Run("ok, target is struct", func(t *testing.T) {
		result := struct {
			V unixTimestampLast `query:"t"`
		}{}
		err := testBindURL("/?t=1710095540&t=1710095541", &result)

		assert.NoError(t, err)
		expect := unixTimestampLast{
			Time: time.Unix(1710095541, 0),
		}
		assert.Equal(t, expect, result.V)
	})

	t.Run("ok, target is an alias to slice and is nil, append multiple inputs", func(t *testing.T) {
		result := struct {
			V IntArrayB `query:"a"`
		}{}
		err := testBindURL("/?a=1,2,3&a=4,5,6", &result)

		assert.NoError(t, err)
		assert.Equal(t, IntArrayB([]int{1, 2, 3, 4, 5, 6}), result.V)
	})

	t.Run("ok, target is an alias to slice and is nil, single input", func(t *testing.T) {
		result := struct {
			V IntArrayB `query:"a"`
		}{}
		err := testBindURL("/?a=1,2", &result)

		assert.NoError(t, err)
		assert.Equal(t, IntArrayB([]int{1, 2}), result.V)
	})

	t.Run("ok, target is pointer an alias to slice and is nil", func(t *testing.T) {
		result := struct {
			V *IntArrayB `query:"a"`
		}{}
		err := testBindURL("/?a=1&a=4,5,6", &result)

		assert.NoError(t, err)
		var expected = IntArrayB([]int{1, 4, 5, 6})
		assert.Equal(t, &expected, result.V)
	})

	t.Run("ok, target is pointer an alias to slice and is NOT nil", func(t *testing.T) {
		result := struct {
			V *IntArrayB `query:"a"`
		}{}
		result.V = new(IntArrayB) // NOT nil

		err := testBindURL("/?a=1&a=4,5,6", &result)
		assert.NoError(t, err)
		var expected = IntArrayB([]int{1, 4, 5, 6})
		assert.Equal(t, &expected, result.V)
	})
}

func TestBindInt8(t *testing.T) {
	t.Run("nok, binding fails", func(t *testing.T) {
		type target struct {
			V int8 `query:"v"`
		}
		p := target{}
		err := testBindURL("/?v=x&v=2", &p)
		assert.EqualError(t, err, "code=400, message=strconv.ParseInt: parsing \"x\": invalid syntax, internal=strconv.ParseInt: parsing \"x\": invalid syntax")
	})

	t.Run("nok, int8 embedded in struct", func(t *testing.T) {
		type target struct {
			int8 `query:"v"` // embedded field is `Anonymous`. We can only set public fields
		}
		p := target{}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{0}, p)
	})

	t.Run("nok, pointer to int8 embedded in struct", func(t *testing.T) {
		type target struct {
			*int8 `query:"v"` // embedded field is `Anonymous`. We can only set public fields
		}
		p := target{}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)

		assert.Equal(t, target{int8: nil}, p)
	})

	t.Run("ok, bind int8 as struct field", func(t *testing.T) {
		type target struct {
			V int8 `query:"v"`
		}
		p := target{V: 127}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{V: 1}, p)
	})

	t.Run("ok, bind pointer to int8 as struct field, value is nil", func(t *testing.T) {
		type target struct {
			V *int8 `query:"v"`
		}
		p := target{}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{V: ptr(int8(1))}, p)
	})

	t.Run("ok, bind pointer to int8 as struct field, value is set", func(t *testing.T) {
		type target struct {
			V *int8 `query:"v"`
		}
		p := target{V: ptr(int8(127))}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{V: ptr(int8(1))}, p)
	})

	t.Run("ok, bind int8 slice as struct field, value is nil", func(t *testing.T) {
		type target struct {
			V []int8 `query:"v"`
		}
		p := target{}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{V: []int8{1, 2}}, p)
	})

	t.Run("ok, bind slice of int8 as struct field, value is set", func(t *testing.T) {
		type target struct {
			V []int8 `query:"v"`
		}
		p := target{V: []int8{111}}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{V: []int8{1, 2}}, p)
	})

	t.Run("ok, bind slice of pointer to int8 as struct field, value is set", func(t *testing.T) {
		type target struct {
			V []*int8 `query:"v"`
		}
		p := target{V: []*int8{ptr(int8(127))}}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{V: []*int8{ptr(int8(1)), ptr(int8(2))}}, p)
	})

	t.Run("ok, bind pointer to slice of int8 as struct field, value is set", func(t *testing.T) {
		type target struct {
			V *[]int8 `query:"v"`
		}
		p := target{V: &[]int8{111}}
		err := testBindURL("/?v=1&v=2", &p)
		assert.NoError(t, err)
		assert.Equal(t, target{V: &[]int8{1, 2}}, p)
	})
}

func TestBindMultipartFormFiles(t *testing.T) {
	file1 := createTestFormFile("file", "file1.txt")
	file11 := createTestFormFile("file", "file11.txt")
	file2 := createTestFormFile("file2", "file2.txt")
	filesA := createTestFormFile("files", "filesA.txt")
	filesB := createTestFormFile("files", "filesB.txt")

	t.Run("nok, can not bind to multipart file struct", func(t *testing.T) {
		var target struct {
			File multipart.FileHeader `form:"file"`
		}
		err := bindMultipartFiles(t, &target, file1, file2) // file2 should be ignored

		assert.EqualError(t, err, "code=400, message=binding to multipart.FileHeader struct is not supported, use pointer to struct, internal=binding to multipart.FileHeader struct is not supported, use pointer to struct")
	})

	t.Run("ok, bind single multipart file to pointer to multipart file", func(t *testing.T) {
		var target struct {
			File *multipart.FileHeader `form:"file"`
		}
		err := bindMultipartFiles(t, &target, file1, file2) // file2 should be ignored

		assert.NoError(t, err)
		assertMultipartFileHeader(t, target.File, file1)
	})

	t.Run("ok, bind multiple multipart files to pointer to multipart file", func(t *testing.T) {
		var target struct {
			File *multipart.FileHeader `form:"file"`
		}
		err := bindMultipartFiles(t, &target, file1, file11)

		assert.NoError(t, err)
		assertMultipartFileHeader(t, target.File, file1) // should choose first one
	})

	t.Run("ok, bind multiple multipart files to slice of multipart file", func(t *testing.T) {
		var target struct {
			Files []multipart.FileHeader `form:"files"`
		}
		err := bindMultipartFiles(t, &target, filesA, filesB, file1)

		assert.NoError(t, err)

		assert.Len(t, target.Files, 2)
		assertMultipartFileHeader(t, &target.Files[0], filesA)
		assertMultipartFileHeader(t, &target.Files[1], filesB)
	})

	t.Run("ok, bind multiple multipart files to slice of pointer to multipart file", func(t *testing.T) {
		var target struct {
			Files []*multipart.FileHeader `form:"files"`
		}
		err := bindMultipartFiles(t, &target, filesA, filesB, file1)

		assert.NoError(t, err)

		assert.Len(t, target.Files, 2)
		assertMultipartFileHeader(t, target.Files[0], filesA)
		assertMultipartFileHeader(t, target.Files[1], filesB)
	})
}

type testFormFile struct {
	Fieldname string
	Filename  string
	Content   []byte
}

func createTestFormFile(formFieldName string, filename string) testFormFile {
	return testFormFile{
		Fieldname: formFieldName,
		Filename:  filename,
		Content:   []byte(strings.Repeat(filename, 10)),
	}
}

func bindMultipartFiles(t *testing.T, target any, files ...testFormFile) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	for _, file := range files {
		fw, err := mw.CreateFormFile(file.Fieldname, file.Filename)
		assert.NoError(t, err)

		n, err := fw.Write(file.Content)
		assert.NoError(t, err)
		assert.Equal(t, len(file.Content), n)
	}

	err := mw.Close()
	assert.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/", &body)
	assert.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rec := httptest.NewRecorder()

	e := New()
	c := e.NewContext(req, rec)
	return c.Bind(target)
}

func assertMultipartFileHeader(t *testing.T, fh *multipart.FileHeader, file testFormFile) {
	assert.Equal(t, file.Filename, fh.Filename)
	assert.Equal(t, int64(len(file.Content)), fh.Size)
	fl, err := fh.Open()
	assert.NoError(t, err)
	body, err := io.ReadAll(fl)
	assert.NoError(t, err)
	assert.Equal(t, string(file.Content), string(body))
	err = fl.Close()
	assert.NoError(t, err)
}

func TestTimeFormatBinding(t *testing.T) {
	type TestStruct struct {
		DateTimeLocal time.Time  `form:"datetime_local" format:"2006-01-02T15:04"`
		Date          time.Time  `query:"date" format:"2006-01-02"`
		CustomFormat  time.Time  `form:"custom" format:"01/02/2006 15:04:05"`
		DefaultTime   time.Time  `form:"default_time"`                      // No format tag - should use default parsing
		PtrTime       *time.Time `query:"ptr_time" format:"2006-01-02"`
	}

	testCases := []struct {
		name        string
		contentType string
		data        string
		queryParams string
		expect      TestStruct
		expectError bool
	}{
		{
			name:        "ok, datetime-local format binding",
			contentType: MIMEApplicationForm,
			data:        "datetime_local=2023-12-25T14:30&default_time=2023-12-25T14:30:45Z",
			expect: TestStruct{
				DateTimeLocal: time.Date(2023, 12, 25, 14, 30, 0, 0, time.UTC),
				DefaultTime:   time.Date(2023, 12, 25, 14, 30, 45, 0, time.UTC),
			},
		},
		{
			name:        "ok, date format binding via query params",
			queryParams: "?date=2023-01-15&ptr_time=2023-02-20",
			expect: TestStruct{
				Date:    time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC),
				PtrTime: &time.Time{},
			},
		},
		{
			name:        "ok, custom format via form data",
			contentType: MIMEApplicationForm,
			data:        "custom=12/25/2023 14:30:45",
			expect: TestStruct{
				CustomFormat: time.Date(2023, 12, 25, 14, 30, 45, 0, time.UTC),
			},
		},
		{
			name:        "nok, invalid format should fail",
			contentType: MIMEApplicationForm,
			data:        "datetime_local=invalid-date",
			expectError: true,
		},
		{
			name:        "nok, wrong format should fail",
			contentType: MIMEApplicationForm,
			data:        "datetime_local=2023-12-25",  // Missing time part
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			var req *http.Request

			if tc.contentType == MIMEApplicationJSON {
				req = httptest.NewRequest(http.MethodPost, "/"+tc.queryParams, strings.NewReader(tc.data))
				req.Header.Set(HeaderContentType, tc.contentType)
			} else if tc.contentType == MIMEApplicationForm {
				req = httptest.NewRequest(http.MethodPost, "/"+tc.queryParams, strings.NewReader(tc.data))
				req.Header.Set(HeaderContentType, tc.contentType)
			} else {
				req = httptest.NewRequest(http.MethodGet, "/"+tc.queryParams, nil)
			}

			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			var result TestStruct
			err := c.Bind(&result)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			// Check individual fields since time comparison can be tricky
			if !tc.expect.DateTimeLocal.IsZero() {
				assert.True(t, tc.expect.DateTimeLocal.Equal(result.DateTimeLocal),
					"DateTimeLocal: expected %v, got %v", tc.expect.DateTimeLocal, result.DateTimeLocal)
			}
			if !tc.expect.Date.IsZero() {
				assert.True(t, tc.expect.Date.Equal(result.Date),
					"Date: expected %v, got %v", tc.expect.Date, result.Date)
			}
			if !tc.expect.CustomFormat.IsZero() {
				assert.True(t, tc.expect.CustomFormat.Equal(result.CustomFormat),
					"CustomFormat: expected %v, got %v", tc.expect.CustomFormat, result.CustomFormat)
			}
			if !tc.expect.DefaultTime.IsZero() {
				assert.True(t, tc.expect.DefaultTime.Equal(result.DefaultTime),
					"DefaultTime: expected %v, got %v", tc.expect.DefaultTime, result.DefaultTime)
			}
			if tc.expect.PtrTime != nil {
				assert.NotNil(t, result.PtrTime)
				if result.PtrTime != nil {
					expectedPtr := time.Date(2023, 2, 20, 0, 0, 0, 0, time.UTC)
					assert.True(t, expectedPtr.Equal(*result.PtrTime),
						"PtrTime: expected %v, got %v", expectedPtr, *result.PtrTime)
				}
			}
		})
	}
}
