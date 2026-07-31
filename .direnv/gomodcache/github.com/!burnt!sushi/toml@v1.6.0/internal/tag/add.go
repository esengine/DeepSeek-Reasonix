package tag

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/BurntSushi/toml/internal"
)

// Add JSON tags to a data structure as expected by toml-test.
func Add(key string, tomlData any) any {
	// Switch on the data type.
	switch orig := tomlData.(type) {
	default:
		panic(fmt.Sprintf("Unknown type: %T", tomlData))

	// A table: we don't need to add any tags, just recurse for every table
	// entry.
	case map[string]any:
		typed := make(map[string]any, len(orig))
		for k, v := range orig {
			typed[k] = Add(k, v)
		}
		return typed

	// An array: we don't need to add any tags, just recurse for every table
	// entry.
	case []map[string]any:
		typed := make([]map[string]any, len(orig))
		for i, v := range orig {
			typed[i] = Add("", v).(map[string]any)
		}
		return typed
	case []any:
		typed := make([]any, len(orig))
		for i, v := range orig {
			typed[i] = Add("", v)
		}
		return typed

	// Datetime: tag as datetime.
	case time.Time:
		switch orig.Location() {
		default:
			return tag("datetime", orig.Format("2006-01-02T15:04:05.999999999Z07:00"))
		case internal.LocalDatetime:
			return tag("datetime-local", orig.Format("2006-01-02T15:04:05.999999999"))
		case internal.LocalDate:
			return tag("date-local", orig.Format("2006-01-02"))
		case internal.LocalTime:
			return tag("time-local", orig.Format("15:04:05.999999999"))
		}

	// Tag primitive values: bool, string, int, and float64.
	case bool:
		return tag("bool", fmt.Sprintf("%v", orig))
	case string:
		return tag("string", orig)
	case int64:
		return tag("integer", fmt.Sprintf("%d", orig))
	case float64:
		switch {
		case math.IsNaN(orig):
			return tag("float", "nan")
		case math.IsInf(orig, 1):
			return tag("float", "inf")
		case math.IsInf(orig, -1):
			return tag("float", "-inf")
		default:
			return tag("float", strconv.FormatFloat(orig, 'g', -1, 64))
		}
	}
}

func tag(typeName string, data any) map[string]any {
	return map[string]any{
		"type":  typeName,
		"value": data,
	}
}
