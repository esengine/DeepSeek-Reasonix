// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"encoding"
	"encoding/xml"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Binder is the interface that wraps the Bind method.
type Binder interface {
	Bind(i interface{}, c Context) error
}

// DefaultBinder is the default implementation of the Binder interface.
type DefaultBinder struct{}

// BindUnmarshaler is the interface used to wrap the UnmarshalParam method.
// Types that don't implement this, but do implement encoding.TextUnmarshaler
// will use that interface instead.
type BindUnmarshaler interface {
	// UnmarshalParam decodes and assigns a value from an form or query param.
	UnmarshalParam(param string) error
}

// bindMultipleUnmarshaler is used by binder to unmarshal multiple values from request at once to
// type implementing this interface. For example request could have multiple query fields `?a=1&a=2&b=test` in that case
// for `a` following slice `["1", "2"] will be passed to unmarshaller.
type bindMultipleUnmarshaler interface {
	UnmarshalParams(params []string) error
}

// BindPathParams binds path params to bindable object
//
// Time format support: time.Time fields can use `format` tags to specify custom parsing layouts.
// Example: `param:"created" format:"2006-01-02T15:04"` for datetime-local format
// Example: `param:"date" format:"2006-01-02"` for date format
// Uses Go's standard time format reference time: Mon Jan 2 15:04:05 MST 2006
// Works with form data, query parameters, and path parameters (not JSON body)
// Falls back to default time.Time parsing if no format tag is specified
func (b *DefaultBinder) BindPathParams(c Context, i interface{}) error {
	names := c.ParamNames()
	values := c.ParamValues()
	params := map[string][]string{}
	for i, name := range names {
		params[name] = []string{values[i]}
	}
	if err := b.bindData(i, params, "param", nil); err != nil {
		return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
	}
	return nil
}

// BindQueryParams binds query params to bindable object
func (b *DefaultBinder) BindQueryParams(c Context, i interface{}) error {
	if err := b.bindData(i, c.QueryParams(), "query", nil); err != nil {
		return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
	}
	return nil
}

// BindBody binds request body contents to bindable object
// NB: then binding forms take note that this implementation uses standard library form parsing
// which parses form data from BOTH URL and BODY if content type is not MIMEMultipartForm
// See non-MIMEMultipartForm: https://golang.org/pkg/net/http/#Request.ParseForm
// See MIMEMultipartForm: https://golang.org/pkg/net/http/#Request.ParseMultipartForm
func (b *DefaultBinder) BindBody(c Context, i interface{}) (err error) {
	req := c.Request()
	if req.ContentLength == 0 {
		return
	}

	// mediatype is found like `mime.ParseMediaType()` does it
	base, _, _ := strings.Cut(req.Header.Get(HeaderContentType), ";")
	mediatype := strings.TrimSpace(base)

	switch mediatype {
	case MIMEApplicationJSON:
		if err = c.Echo().JSONSerializer.Deserialize(c, i); err != nil {
			switch err.(type) {
			case *HTTPError:
				return err
			default:
				return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
			}
		}
	case MIMEApplicationXML, MIMETextXML:
		if err = xml.NewDecoder(req.Body).Decode(i); err != nil {
			if ute, ok := err.(*xml.UnsupportedTypeError); ok {
				return NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Unsupported type error: type=%v, error=%v", ute.Type, ute.Error())).SetInternal(err)
			} else if se, ok := err.(*xml.SyntaxError); ok {
				return NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Syntax error: line=%v, error=%v", se.Line, se.Error())).SetInternal(err)
			}
			return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
		}
	case MIMEApplicationForm:
		params, err := c.FormParams()
		if err != nil {
			return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
		}
		if err = b.bindData(i, params, "form", nil); err != nil {
			return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
		}
	case MIMEMultipartForm:
		params, err := c.MultipartForm()
		if err != nil {
			return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
		}
		if err = b.bindData(i, params.Value, "form", params.File); err != nil {
			return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
		}
	default:
		return ErrUnsupportedMediaType
	}
	return nil
}

// BindHeaders binds HTTP headers to a bindable object
func (b *DefaultBinder) BindHeaders(c Context, i interface{}) error {
	if err := b.bindData(i, c.Request().Header, "header", nil); err != nil {
		return NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
	}
	return nil
}

// Bind implements the `Binder#Bind` function.
// Binding is done in following order: 1) path params; 2) query params; 3) request body. Each step COULD override previous
// step binded values. For single source binding use their own methods BindBody, BindQueryParams, BindPathParams.
func (b *DefaultBinder) Bind(i interface{}, c Context) (err error) {
	if err := b.BindPathParams(c, i); err != nil {
		return err
	}
	// Only bind query parameters for GET/DELETE/HEAD to avoid unexpected behavior with destination struct binding from body.
	// For example a request URL `&id=1&lang=en` with body `{"id":100,"lang":"de"}` would lead to precedence issues.
	// The HTTP method check restores pre-v4.1.11 behavior to avoid these problems (see issue #1670)
	method := c.Request().Method
	if method == http.MethodGet || method == http.MethodDelete || method == http.MethodHead {
		if err = b.BindQueryParams(c, i); err != nil {
			return err
		}
	}
	return b.BindBody(c, i)
}

// bindData will bind data ONLY fields in destination struct that have EXPLICIT tag
func (b *DefaultBinder) bindData(destination interface{}, data map[string][]string, tag string, dataFiles map[string][]*multipart.FileHeader) error {
	if destination == nil || (len(data) == 0 && len(dataFiles) == 0) {
		return nil
	}
	hasFiles := len(dataFiles) > 0
	typ := reflect.TypeOf(destination).Elem()
	val := reflect.ValueOf(destination).Elem()

	// Support binding to limited Map destinations:
	// - map[string][]string,
	// - map[string]string <-- (binds first value from data slice)
	// - map[string]interface{}
	// You are better off binding to struct but there are user who want this map feature. Source of data for these cases are:
	// params,query,header,form as these sources produce string values, most of the time slice of strings, actually.
	if typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.String {
		k := typ.Elem().Kind()
		isElemInterface := k == reflect.Interface
		isElemString := k == reflect.String
		isElemSliceOfStrings := k == reflect.Slice && typ.Elem().Elem().Kind() == reflect.String
		if !(isElemSliceOfStrings || isElemString || isElemInterface) {
			return nil
		}
		if val.IsNil() {
			val.Set(reflect.MakeMap(typ))
		}
		for k, v := range data {
			if isElemString {
				val.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(v[0]))
			} else if isElemInterface {
				// To maintain backward compatibility, we always bind to the first string value
				// and not the slice of strings when dealing with map[string]interface{}{}
				val.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(v[0]))
			} else {
				val.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(v))
			}
		}
		return nil
	}

	// !struct
	if typ.Kind() != reflect.Struct {
		if tag == "param" || tag == "query" || tag == "header" {
			// incompatible type, data is probably to be found in the body
			return nil
		}
		return errors.New("binding element must be a struct")
	}

	for i := 0; i < typ.NumField(); i++ { // iterate over all destination fields
		typeField := typ.Field(i)
		structField := val.Field(i)
		if typeField.Anonymous {
			if structField.Kind() == reflect.Ptr {
				structField = structField.Elem()
			}
		}
		if !structField.CanSet() {
			continue
		}
		structFieldKind := structField.Kind()
		inputFieldName := typeField.Tag.Get(tag)
		if typeField.Anonymous && structFieldKind == reflect.Struct && inputFieldName != "" {
			// if anonymous struct with query/param/form tags, report an error
			return errors.New("query/param/form tags are not allowed with anonymous struct field")
		}

		if inputFieldName == "" {
			// If tag is nil, we inspect if the field is a not BindUnmarshaler struct and try to bind data into it (might contain fields with tags).
			// structs that implement BindUnmarshaler are bound only when they have explicit tag
			if _, ok := structField.Addr().Interface().(BindUnmarshaler); !ok && structFieldKind == reflect.Struct {
				if err := b.bindData(structField.Addr().Interface(), data, tag, dataFiles); err != nil {
					return err
				}
			}
			// does not have explicit tag and is not an ordinary struct - so move to next field
			continue
		}

		if hasFiles {
			if ok, err := isFieldMultipartFile(structField.Type()); err != nil {
				return err
			} else if ok {
				if ok := setMultipartFileHeaderTypes(structField, inputFieldName, dataFiles); ok {
					continue
				}
			}
		}

		inputValue, exists := data[inputFieldName]
		if !exists {
			// Go json.Unmarshal supports case-insensitive binding.  However the
			// url params are bound case-sensitive which is inconsistent.  To
			// fix this we must check all of the map values in a
			// case-insensitive search.
			for k, v := range data {
				if strings.EqualFold(k, inputFieldName) {
					inputValue = v
					exists = true
					break
				}
			}
		}

		if !exists {
			continue
		}

		// NOTE: algorithm here is not particularly sophisticated. It probably does not work with absurd types like `**[]*int`
		// but it is smart enough to handle niche cases like `*int`,`*[]string`,`[]*int` .

		// try unmarshalling first, in case we're dealing with an alias to an array type
		if ok, err := unmarshalInputsToField(typeField.Type.Kind(), inputValue, structField); ok {
			if err != nil {
				return err
			}
			continue
		}

		formatTag := typeField.Tag.Get("format")
		if ok, err := unmarshalInputToField(typeField.Type.Kind(), inputValue[0], structField, formatTag); ok {
			if err != nil {
				return err
			}
			continue
		}

		// we could be dealing with pointer to slice `*[]string` so dereference it. There are weird OpenAPI generators
		// that could create struct fields like that.
		if structFieldKind == reflect.Pointer {
			structFieldKind = structField.Elem().Kind()
			structField = structField.Elem()
		}

		if structFieldKind == reflect.Slice {
			sliceOf := structField.Type().Elem().Kind()
			numElems := len(inputValue)
			slice := reflect.MakeSlice(structField.Type(), numElems, numElems)
			for j := 0; j < numElems; j++ {
				if err := setWithProperType(sliceOf, inputValue[j], slice.Index(j)); err != nil {
					return err
				}
			}
			structField.Set(slice)
			continue
		}

		if err := setWithProperType(structFieldKind, inputValue[0], structField); err != nil {
			return err
		}
	}
	return nil
}

func setWithProperType(valueKind reflect.Kind, val string, structField reflect.Value) error {
	// But also call it here, in case we're dealing with an array of BindUnmarshalers
	// Note: format tag not available in this context, so empty string is passed
	if ok, err := unmarshalInputToField(valueKind, val, structField, ""); ok {
		return err
	}

	switch valueKind {
	case reflect.Ptr:
		return setWithProperType(structField.Elem().Kind(), val, structField.Elem())
	case reflect.Int:
		return setIntField(val, 0, structField)
	case reflect.Int8:
		return setIntField(val, 8, structField)
	case reflect.Int16:
		return setIntField(val, 16, structField)
	case reflect.Int32:
		return setIntField(val, 32, structField)
	case reflect.Int64:
		return setIntField(val, 64, structField)
	case reflect.Uint:
		return setUintField(val, 0, structField)
	case reflect.Uint8:
		return setUintField(val, 8, structField)
	case reflect.Uint16:
		return setUintField(val, 16, structField)
	case reflect.Uint32:
		return setUintField(val, 32, structField)
	case reflect.Uint64:
		return setUintField(val, 64, structField)
	case reflect.Bool:
		return setBoolField(val, structField)
	case reflect.Float32:
		return setFloatField(val, 32, structField)
	case reflect.Float64:
		return setFloatField(val, 64, structField)
	case reflect.String:
		structField.SetString(val)
	default:
		return errors.New("unknown type")
	}
	return nil
}

func unmarshalInputsToField(valueKind reflect.Kind, values []string, field reflect.Value) (bool, error) {
	if valueKind == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	fieldIValue := field.Addr().Interface()
	unmarshaler, ok := fieldIValue.(bindMultipleUnmarshaler)
	if !ok {
		return false, nil
	}
	return true, unmarshaler.UnmarshalParams(values)
}

func unmarshalInputToField(valueKind reflect.Kind, val string, field reflect.Value, formatTag string) (bool, error) {
	if valueKind == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	fieldIValue := field.Addr().Interface()

	// Handle time.Time with custom format tag
	if formatTag != "" {
		if _, isTime := fieldIValue.(*time.Time); isTime {
			t, err := time.Parse(formatTag, val)
			if err != nil {
				return true, err
			}
			field.Set(reflect.ValueOf(t))
			return true, nil
		}
	}

	switch unmarshaler := fieldIValue.(type) {
	case BindUnmarshaler:
		return true, unmarshaler.UnmarshalParam(val)
	case encoding.TextUnmarshaler:
		return true, unmarshaler.UnmarshalText([]byte(val))
	}

	return false, nil
}

func setIntField(value string, bitSize int, field reflect.Value) error {
	if value == "" {
		value = "0"
	}
	intVal, err := strconv.ParseInt(value, 10, bitSize)
	if err == nil {
		field.SetInt(intVal)
	}
	return err
}

func setUintField(value string, bitSize int, field reflect.Value) error {
	if value == "" {
		value = "0"
	}
	uintVal, err := strconv.ParseUint(value, 10, bitSize)
	if err == nil {
		field.SetUint(uintVal)
	}
	return err
}

func setBoolField(value string, field reflect.Value) error {
	if value == "" {
		value = "false"
	}
	boolVal, err := strconv.ParseBool(value)
	if err == nil {
		field.SetBool(boolVal)
	}
	return err
}

func setFloatField(value string, bitSize int, field reflect.Value) error {
	if value == "" {
		value = "0.0"
	}
	floatVal, err := strconv.ParseFloat(value, bitSize)
	if err == nil {
		field.SetFloat(floatVal)
	}
	return err
}

var (
	// NOT supported by bind as you can NOT check easily empty struct being actual file or not
	multipartFileHeaderType = reflect.TypeFor[multipart.FileHeader]()
	// supported by bind as you can check by nil value if file existed or not
	multipartFileHeaderPointerType      = reflect.TypeFor[*multipart.FileHeader]()
	multipartFileHeaderSliceType        = reflect.TypeFor[[]multipart.FileHeader]()
	multipartFileHeaderPointerSliceType = reflect.TypeFor[[]*multipart.FileHeader]()
)

func isFieldMultipartFile(field reflect.Type) (bool, error) {
	switch field {
	case multipartFileHeaderPointerType,
		multipartFileHeaderSliceType,
		multipartFileHeaderPointerSliceType:
		return true, nil
	case multipartFileHeaderType:
		return true, errors.New("binding to multipart.FileHeader struct is not supported, use pointer to struct")
	default:
		return false, nil
	}
}

func setMultipartFileHeaderTypes(structField reflect.Value, inputFieldName string, files map[string][]*multipart.FileHeader) bool {
	fileHeaders := files[inputFieldName]
	if len(fileHeaders) == 0 {
		return false
	}

	result := true
	switch structField.Type() {
	case multipartFileHeaderPointerSliceType:
		structField.Set(reflect.ValueOf(fileHeaders))
	case multipartFileHeaderSliceType:
		headers := make([]multipart.FileHeader, len(fileHeaders))
		for i, fileHeader := range fileHeaders {
			headers[i] = *fileHeader
		}
		structField.Set(reflect.ValueOf(headers))
	case multipartFileHeaderPointerType:
		structField.Set(reflect.ValueOf(fileHeaders[0]))
	default:
		result = false
	}

	return result
}
