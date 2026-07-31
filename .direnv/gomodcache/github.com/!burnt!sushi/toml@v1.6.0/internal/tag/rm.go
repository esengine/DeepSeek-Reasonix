package tag

import (
	"fmt"
	"strconv"
	"time"

	"github.com/BurntSushi/toml/internal"
)

// Remove JSON tags to a data structure as returned by toml-test.
func Remove(typedJson any) (any, error) {
	// Switch on the data type.
	switch v := typedJson.(type) {

	// Object: this can either be a TOML table or a primitive with tags.
	case map[string]any:
		// This value represents a primitive: remove the tags and return just
		// the primitive value.
		if len(v) == 2 && in("type", v) && in("value", v) {
			ut, err := untag(v)
			if err != nil {
				return ut, fmt.Errorf("tag.Remove: %w", err)
			}
			return ut, nil
		}

		// Table: remove tags on all children.
		m := make(map[string]any, len(v))
		for k, v2 := range v {
			var err error
			m[k], err = Remove(v2)
			if err != nil {
				return nil, err
			}
		}
		return m, nil

	// Array: remove tags from all items.
	case []any:
		a := make([]any, len(v))
		for i := range v {
			var err error
			a[i], err = Remove(v[i])
			if err != nil {
				return nil, err
			}
		}
		return a, nil
	}

	// The top level must be an object or array.
	return nil, fmt.Errorf("tag.Remove: unrecognized JSON format '%T'", typedJson)
}

// Check if key is in the table m.
func in(key string, m map[string]any) bool {
	_, ok := m[key]
	return ok
}

// Return a primitive: read the "type" and convert the "value" to that.
func untag(typed map[string]any) (any, error) {
	t := typed["type"].(string)
	v := typed["value"].(string)
	switch t {
	case "string":
		return v, nil
	case "integer":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("untag: %w", err)
		}
		return n, nil
	case "float":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("untag: %w", err)
		}
		return f, nil
	case "datetime":
		return parseTime(v, "2006-01-02T15:04:05.999999999Z07:00", nil)
	case "datetime-local":
		return parseTime(v, "2006-01-02T15:04:05.999999999", internal.LocalDatetime)
	case "date-local":
		return parseTime(v, "2006-01-02", internal.LocalDate)
	case "time-local":
		return parseTime(v, "15:04:05.999999999", internal.LocalTime)
	case "bool":
		switch v {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("untag: could not parse %q as a boolean", v)
	}

	return nil, fmt.Errorf("untag: unrecognized tag type %q", t)
}

func parseTime(v, format string, l *time.Location) (time.Time, error) {
	t, err := time.Parse(format, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not parse %q as a datetime: %w", v, err)
	}
	if l != nil {
		t = t.In(l)
	}
	return t, nil
}
