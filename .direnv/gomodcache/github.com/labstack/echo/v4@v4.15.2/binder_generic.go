// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"encoding"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// TimeLayout specifies the format for parsing time values in request parameters.
// It can be a standard Go time layout string or one of the special Unix time layouts.
type TimeLayout string

// TimeOpts is options for parsing time.Time values
type TimeOpts struct {
	// Layout specifies the format for parsing time values in request parameters.
	// It can be a standard Go time layout string or one of the special Unix time layouts.
	//
	// Parsing layout defaults to: echo.TimeLayout(time.RFC3339Nano)
	// - To convert to custom layout use `echo.TimeLayout("2006-01-02")`
	// - To convert unix timestamp (integer) to time.Time use `echo.TimeLayoutUnixTime`
	// - To convert unix timestamp in milliseconds to time.Time use `echo.TimeLayoutUnixTimeMilli`
	// - To convert unix timestamp in nanoseconds to time.Time use `echo.TimeLayoutUnixTimeNano`
	Layout TimeLayout

	// ParseInLocation is location used with time.ParseInLocation for layout that do not contain
	// timezone information to set output time in given location.
	// Defaults to time.UTC
	ParseInLocation *time.Location

	// ToInLocation is location to which parsed time is converted to after parsing.
	// The parsed time will be converted using time.In(ToInLocation).
	// Defaults to time.UTC
	ToInLocation *time.Location
}

// TimeLayout constants for parsing Unix timestamps in different precisions.
const (
	TimeLayoutUnixTime      = TimeLayout("UnixTime")      // Unix timestamp in seconds
	TimeLayoutUnixTimeMilli = TimeLayout("UnixTimeMilli") // Unix timestamp in milliseconds
	TimeLayoutUnixTimeNano  = TimeLayout("UnixTimeNano")  // Unix timestamp in nanoseconds
)

// PathParam extracts and parses a path parameter from the context by name.
// It returns the typed value and an error if binding fails. Returns ErrNonExistentKey if parameter not found.
//
// Empty String Handling:
//
//	If the parameter exists but has an empty value, the zero value of type T is returned
//	with no error. For example, a path parameter with value "" returns (0, nil) for int types.
//	This differs from standard library behavior where parsing empty strings returns errors.
//	To treat empty values as errors, validate the result separately or check the raw value.
//
// See ParseValue for supported types and options
func PathParam[T any](c Context, paramName string, opts ...any) (T, error) {
	for i, name := range c.ParamNames() {
		if name == paramName {
			pValues := c.ParamValues()
			v, err := ParseValue[T](pValues[i], opts...)
			if err != nil {
				return v, NewBindingError(paramName, []string{pValues[i]}, "path param", err)
			}
			return v, nil
		}
	}
	var zero T
	return zero, ErrNonExistentKey
}

// PathParamOr extracts and parses a path parameter from the context by name.
// Returns defaultValue if the parameter is not found or has an empty value.
// Returns an error only if parsing fails (e.g., "abc" for int type).
//
// Example:
//
//	id, err := echo.PathParamOr[int](c, "id", 0)
//	// If "id" is missing: returns (0, nil)
//	// If "id" is "123": returns (123, nil)
//	// If "id" is "abc": returns (0, BindingError)
//
// See ParseValue for supported types and options
func PathParamOr[T any](c Context, paramName string, defaultValue T, opts ...any) (T, error) {
	for i, name := range c.ParamNames() {
		if name == paramName {
			pValues := c.ParamValues()
			v, err := ParseValueOr[T](pValues[i], defaultValue, opts...)
			if err != nil {
				return v, NewBindingError(paramName, []string{pValues[i]}, "path param", err)
			}
			return v, nil
		}
	}
	return defaultValue, nil
}

// QueryParam extracts and parses a single query parameter from the request by key.
// It returns the typed value and an error if binding fails. Returns ErrNonExistentKey if parameter not found.
//
// Empty String Handling:
//
//	If the parameter exists but has an empty value (?key=), the zero value of type T is returned
//	with no error. For example, "?count=" returns (0, nil) for int types.
//	This differs from standard library behavior where parsing empty strings returns errors.
//	To treat empty values as errors, validate the result separately or check the raw value.
//
// Behavior Summary:
//   - Missing key (?other=value): returns (zero, ErrNonExistentKey)
//   - Empty value (?key=): returns (zero, nil)
//   - Invalid value (?key=abc for int): returns (zero, BindingError)
//
// See ParseValue for supported types and options
func QueryParam[T any](c Context, key string, opts ...any) (T, error) {
	values, ok := c.QueryParams()[key]
	if !ok {
		var zero T
		return zero, ErrNonExistentKey
	}
	if len(values) == 0 {
		var zero T
		return zero, nil
	}
	value := values[0]
	v, err := ParseValue[T](value, opts...)
	if err != nil {
		return v, NewBindingError(key, []string{value}, "query param", err)
	}
	return v, nil
}

// QueryParamOr extracts and parses a single query parameter from the request by key.
// Returns defaultValue if the parameter is not found or has an empty value.
// Returns an error only if parsing fails (e.g., "abc" for int type).
//
// Example:
//
//	page, err := echo.QueryParamOr[int](c, "page", 1)
//	// If "page" is missing: returns (1, nil)
//	// If "page" is "5": returns (5, nil)
//	// If "page" is "abc": returns (1, BindingError)
//
// See ParseValue for supported types and options
func QueryParamOr[T any](c Context, key string, defaultValue T, opts ...any) (T, error) {
	values, ok := c.QueryParams()[key]
	if !ok {
		return defaultValue, nil
	}
	if len(values) == 0 {
		return defaultValue, nil
	}
	value := values[0]
	v, err := ParseValueOr[T](value, defaultValue, opts...)
	if err != nil {
		return v, NewBindingError(key, []string{value}, "query param", err)
	}
	return v, nil
}

// QueryParams extracts and parses all values for a query parameter key as a slice.
// It returns the typed slice and an error if binding any value fails. Returns ErrNonExistentKey if parameter not found.
//
// See ParseValues for supported types and options
func QueryParams[T any](c Context, key string, opts ...any) ([]T, error) {
	values, ok := c.QueryParams()[key]
	if !ok {
		return nil, ErrNonExistentKey
	}

	result, err := ParseValues[T](values, opts...)
	if err != nil {
		return nil, NewBindingError(key, values, "query params", err)
	}
	return result, nil
}

// QueryParamsOr extracts and parses all values for a query parameter key as a slice.
// Returns defaultValue if the parameter is not found.
// Returns an error only if parsing any value fails.
//
// Example:
//
//	ids, err := echo.QueryParamsOr[int](c, "ids", []int{})
//	// If "ids" is missing: returns ([], nil)
//	// If "ids" is "1&ids=2": returns ([1, 2], nil)
//	// If "ids" contains "abc": returns ([], BindingError)
//
// See ParseValues for supported types and options
func QueryParamsOr[T any](c Context, key string, defaultValue []T, opts ...any) ([]T, error) {
	values, ok := c.QueryParams()[key]
	if !ok {
		return defaultValue, nil
	}

	result, err := ParseValuesOr[T](values, defaultValue, opts...)
	if err != nil {
		return nil, NewBindingError(key, values, "query params", err)
	}
	return result, nil
}

// FormParam extracts and parses a single form value from the request by key.
// It returns the typed value and an error if binding fails. Returns ErrNonExistentKey if parameter not found.
//
// Empty String Handling:
//
//	If the form field exists but has an empty value, the zero value of type T is returned
//	with no error. For example, an empty form field returns (0, nil) for int types.
//	This differs from standard library behavior where parsing empty strings returns errors.
//	To treat empty values as errors, validate the result separately or check the raw value.
//
// See ParseValue for supported types and options
func FormParam[T any](c Context, key string, opts ...any) (T, error) {
	formValues, err := c.FormParams()
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to parse form param, key: %s, err: %w", key, err)
	}
	values, ok := formValues[key]
	if !ok {
		var zero T
		return zero, ErrNonExistentKey
	}
	if len(values) == 0 {
		var zero T
		return zero, nil
	}
	value := values[0]
	v, err := ParseValue[T](value, opts...)
	if err != nil {
		return v, NewBindingError(key, []string{value}, "form param", err)
	}
	return v, nil
}

// FormParamOr extracts and parses a single form value from the request by key.
// Returns defaultValue if the parameter is not found or has an empty value.
// Returns an error only if parsing fails or form parsing errors occur.
//
// Example:
//
//	limit, err := echo.FormValueOr[int](c, "limit", 100)
//	// If "limit" is missing: returns (100, nil)
//	// If "limit" is "50": returns (50, nil)
//	// If "limit" is "abc": returns (100, BindingError)
//
// See ParseValue for supported types and options
func FormParamOr[T any](c Context, key string, defaultValue T, opts ...any) (T, error) {
	formValues, err := c.FormParams()
	if err != nil {
		var zero T
		return zero, fmt.Errorf("failed to parse form param, key: %s, err: %w", key, err)
	}
	values, ok := formValues[key]
	if !ok {
		return defaultValue, nil
	}
	if len(values) == 0 {
		return defaultValue, nil
	}
	value := values[0]
	v, err := ParseValueOr[T](value, defaultValue, opts...)
	if err != nil {
		return v, NewBindingError(key, []string{value}, "form param", err)
	}
	return v, nil
}

// FormParams extracts and parses all values for a form values key as a slice.
// It returns the typed slice and an error if binding any value fails. Returns ErrNonExistentKey if parameter not found.
//
// See ParseValues for supported types and options
func FormParams[T any](c Context, key string, opts ...any) ([]T, error) {
	formValues, err := c.FormParams()
	if err != nil {
		return nil, fmt.Errorf("failed to parse form params, key: %s, err: %w", key, err)
	}
	values, ok := formValues[key]
	if !ok {
		return nil, ErrNonExistentKey
	}
	result, err := ParseValues[T](values, opts...)
	if err != nil {
		return nil, NewBindingError(key, values, "form params", err)
	}
	return result, nil
}

// FormParamsOr extracts and parses all values for a form values key as a slice.
// Returns defaultValue if the parameter is not found.
// Returns an error only if parsing any value fails or form parsing errors occur.
//
// Example:
//
//	tags, err := echo.FormParamsOr[string](c, "tags", []string{})
//	// If "tags" is missing: returns ([], nil)
//	// If form parsing fails: returns (nil, error)
//
// See ParseValues for supported types and options
func FormParamsOr[T any](c Context, key string, defaultValue []T, opts ...any) ([]T, error) {
	formValues, err := c.FormParams()
	if err != nil {
		return nil, fmt.Errorf("failed to parse form params, key: %s, err: %w", key, err)
	}
	values, ok := formValues[key]
	if !ok {
		return defaultValue, nil
	}
	result, err := ParseValuesOr[T](values, defaultValue, opts...)
	if err != nil {
		return nil, NewBindingError(key, values, "form params", err)
	}
	return result, nil
}

// ParseValues parses value to generic type slice. Same types are supported as ParseValue
// function but the result type is slice instead of scalar value.
//
// See ParseValue for supported types and options
func ParseValues[T any](values []string, opts ...any) ([]T, error) {
	var zero []T
	return ParseValuesOr(values, zero, opts...)
}

// ParseValuesOr parses value to generic type slice, when value is empty defaultValue is returned.
// Same types are supported as ParseValue function but the result type is slice instead of scalar value.
//
// See ParseValue for supported types and options
func ParseValuesOr[T any](values []string, defaultValue []T, opts ...any) ([]T, error) {
	if len(values) == 0 {
		return defaultValue, nil
	}
	result := make([]T, 0, len(values))
	for _, v := range values {
		tmp, err := ParseValue[T](v, opts...)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp)
	}
	return result, nil
}

// ParseValue parses value to generic type
//
// Types that are supported:
//   - bool
//   - float32
//   - float64
//   - int
//   - int8
//   - int16
//   - int32
//   - int64
//   - uint
//   - uint8/byte
//   - uint16
//   - uint32
//   - uint64
//   - string
//   - echo.BindUnmarshaler interface
//   - encoding.TextUnmarshaler interface
//   - json.Unmarshaler interface
//   - time.Duration
//   - time.Time use echo.TimeOpts or echo.TimeLayout to set time parsing configuration
func ParseValue[T any](value string, opts ...any) (T, error) {
	var zero T
	return ParseValueOr(value, zero, opts...)
}

// ParseValueOr parses value to generic type, when value is empty defaultValue is returned.
//
// Types that are supported:
//   - bool
//   - float32
//   - float64
//   - int
//   - int8
//   - int16
//   - int32
//   - int64
//   - uint
//   - uint8/byte
//   - uint16
//   - uint32
//   - uint64
//   - string
//   - echo.BindUnmarshaler interface
//   - encoding.TextUnmarshaler interface
//   - json.Unmarshaler interface
//   - time.Duration
//   - time.Time use echo.TimeOpts or echo.TimeLayout to set time parsing configuration
func ParseValueOr[T any](value string, defaultValue T, opts ...any) (T, error) {
	if len(value) == 0 {
		return defaultValue, nil
	}
	var tmp T
	if err := bindValue(value, &tmp, opts...); err != nil {
		var zero T
		return zero, fmt.Errorf("failed to parse value, err: %w", err)
	}
	return tmp, nil
}

func bindValue(value string, dest any, opts ...any) error {
	// NOTE: if this function is ever made public the dest should be checked for nil
	// values when dealing with interfaces
	if len(opts) > 0 {
		if _, isTime := dest.(*time.Time); !isTime {
			return fmt.Errorf("options are only supported for time.Time, got %T", dest)
		}
	}

	switch d := dest.(type) {
	case *bool:
		n, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		*d = n
	case *float32:
		n, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return err
		}
		*d = float32(n)
	case *float64:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		*d = n
	case *int:
		n, err := strconv.ParseInt(value, 10, 0)
		if err != nil {
			return err
		}
		*d = int(n)
	case *int8:
		n, err := strconv.ParseInt(value, 10, 8)
		if err != nil {
			return err
		}
		*d = int8(n)
	case *int16:
		n, err := strconv.ParseInt(value, 10, 16)
		if err != nil {
			return err
		}
		*d = int16(n)
	case *int32:
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		*d = int32(n)
	case *int64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		*d = n
	case *uint:
		n, err := strconv.ParseUint(value, 10, 0)
		if err != nil {
			return err
		}
		*d = uint(n)
	case *uint8:
		n, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return err
		}
		*d = uint8(n)
	case *uint16:
		n, err := strconv.ParseUint(value, 10, 16)
		if err != nil {
			return err
		}
		*d = uint16(n)
	case *uint32:
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return err
		}
		*d = uint32(n)
	case *uint64:
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}
		*d = n
	case *string:
		*d = value
	case *time.Duration:
		t, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = t
	case *time.Time:
		to := TimeOpts{
			Layout:          TimeLayout(time.RFC3339Nano),
			ParseInLocation: time.UTC,
			ToInLocation:    time.UTC,
		}
		for _, o := range opts {
			switch v := o.(type) {
			case TimeOpts:
				if v.Layout != "" {
					to.Layout = v.Layout
				}
				if v.ParseInLocation != nil {
					to.ParseInLocation = v.ParseInLocation
				}
				if v.ToInLocation != nil {
					to.ToInLocation = v.ToInLocation
				}
			case TimeLayout:
				to.Layout = v
			}
		}
		var t time.Time
		var err error
		switch to.Layout {
		case TimeLayoutUnixTime:
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			t = time.Unix(n, 0)
		case TimeLayoutUnixTimeMilli:
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			t = time.UnixMilli(n)
		case TimeLayoutUnixTimeNano:
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			t = time.Unix(0, n)
		default:
			if to.ParseInLocation != nil {
				t, err = time.ParseInLocation(string(to.Layout), value, to.ParseInLocation)
			} else {
				t, err = time.Parse(string(to.Layout), value)
			}
			if err != nil {
				return err
			}
		}
		*d = t.In(to.ToInLocation)
	case BindUnmarshaler:
		if err := d.UnmarshalParam(value); err != nil {
			return err
		}
	case encoding.TextUnmarshaler:
		if err := d.UnmarshalText([]byte(value)); err != nil {
			return err
		}
	case json.Unmarshaler:
		if err := d.UnmarshalJSON([]byte(value)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported value type: %T", dest)
	}
	return nil
}
