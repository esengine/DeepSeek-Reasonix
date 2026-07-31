package bytes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type (
	// Bytes struct
	Bytes struct{}
)

// binary units (IEC 60027)
const (
	_ = 1.0 << (10 * iota) // ignore first value by assigning to blank identifier
	KiB
	MiB
	GiB
	TiB
	PiB
	EiB
)

// decimal units (SI international system of units)
const (
	KB = 1000
	MB = KB * 1000
	GB = MB * 1000
	TB = GB * 1000
	PB = TB * 1000
	EB = PB * 1000
)

var (
	patternBinary  = regexp.MustCompile(`(?i)^(-?\d+(?:\.\d+)?)\s?([KMGTPE]iB?)$`)
	patternDecimal = regexp.MustCompile(`(?i)^(-?\d+(?:\.\d+)?)\s?([KMGTPE]B?|B?)$`)
	global         = New()
)

// New creates a Bytes instance.
func New() *Bytes {
	return &Bytes{}
}

// Format formats bytes integer to human readable string according to IEC 60027.
// For example, 31323 bytes will return 30.59KB.
func (b *Bytes) Format(value int64) string {
	return b.FormatBinary(value)
}

// FormatBinary formats bytes integer to human readable string according to IEC 60027.
// For example, 31323 bytes will return 30.59KB.
func (*Bytes) FormatBinary(value int64) string {
	multiple := ""
	val := float64(value)

	switch {
	case value >= EiB:
		val /= EiB
		multiple = "EiB"
	case value >= PiB:
		val /= PiB
		multiple = "PiB"
	case value >= TiB:
		val /= TiB
		multiple = "TiB"
	case value >= GiB:
		val /= GiB
		multiple = "GiB"
	case value >= MiB:
		val /= MiB
		multiple = "MiB"
	case value >= KiB:
		val /= KiB
		multiple = "KiB"
	case value == 0:
		return "0"
	default:
		return strconv.FormatInt(value, 10) + "B"
	}

	return fmt.Sprintf("%.2f%s", val, multiple)
}

// FormatDecimal formats bytes integer to human readable string according to SI international system of units.
// For example, 31323 bytes will return 31.32KB.
func (*Bytes) FormatDecimal(value int64) string {
	multiple := ""
	val := float64(value)

	switch {
	case value >= EB:
		val /= EB
		multiple = "EB"
	case value >= PB:
		val /= PB
		multiple = "PB"
	case value >= TB:
		val /= TB
		multiple = "TB"
	case value >= GB:
		val /= GB
		multiple = "GB"
	case value >= MB:
		val /= MB
		multiple = "MB"
	case value >= KB:
		val /= KB
		multiple = "KB"
	case value == 0:
		return "0"
	default:
		return strconv.FormatInt(value, 10) + "B"
	}

	return fmt.Sprintf("%.2f%s", val, multiple)
}

// Parse parses human readable bytes string to bytes integer.
// For example, 6GiB (6Gi is also valid) will return 6442450944, and
// 6GB (6G is also valid) will return 6000000000.
func (b *Bytes) Parse(value string) (int64, error) {

	i, err := b.ParseBinary(value)
	if err == nil {
		return i, err
	}

	return b.ParseDecimal(value)
}

// ParseBinary parses human readable bytes string to bytes integer.
// For example, 6GiB (6Gi is also valid) will return 6442450944.
func (*Bytes) ParseBinary(value string) (i int64, err error) {
	parts := patternBinary.FindStringSubmatch(value)
	if len(parts) < 3 {
		return 0, fmt.Errorf("error parsing value=%s", value)
	}
	bytesString := parts[1]
	multiple := strings.ToUpper(parts[2])
	bytes, err := strconv.ParseFloat(bytesString, 64)
	if err != nil {
		return
	}

	switch multiple {
	case "KI", "KIB":
		return int64(bytes * KiB), nil
	case "MI", "MIB":
		return int64(bytes * MiB), nil
	case "GI", "GIB":
		return int64(bytes * GiB), nil
	case "TI", "TIB":
		return int64(bytes * TiB), nil
	case "PI", "PIB":
		return int64(bytes * PiB), nil
	case "EI", "EIB":
		return int64(bytes * EiB), nil
	default:
		return int64(bytes), nil
	}
}

// ParseDecimal parses human readable bytes string to bytes integer.
// For example, 6GB (6G is also valid) will return 6000000000.
func (*Bytes) ParseDecimal(value string) (i int64, err error) {
	parts := patternDecimal.FindStringSubmatch(value)
	if len(parts) < 3 {
		return 0, fmt.Errorf("error parsing value=%s", value)
	}
	bytesString := parts[1]
	multiple := strings.ToUpper(parts[2])
	bytes, err := strconv.ParseFloat(bytesString, 64)
	if err != nil {
		return
	}

	switch multiple {
	case "K", "KB":
		return int64(bytes * KB), nil
	case "M", "MB":
		return int64(bytes * MB), nil
	case "G", "GB":
		return int64(bytes * GB), nil
	case "T", "TB":
		return int64(bytes * TB), nil
	case "P", "PB":
		return int64(bytes * PB), nil
	case "E", "EB":
		return int64(bytes * EB), nil
	default:
		return int64(bytes), nil
	}
}

// Format wraps global Bytes's Format function.
func Format(value int64) string {
	return global.Format(value)
}

// FormatBinary wraps global Bytes's FormatBinary function.
func FormatBinary(value int64) string {
	return global.FormatBinary(value)
}

// FormatDecimal wraps global Bytes's FormatDecimal function.
func FormatDecimal(value int64) string {
	return global.FormatDecimal(value)
}

// Parse wraps global Bytes's Parse function.
func Parse(value string) (int64, error) {
	return global.Parse(value)
}
