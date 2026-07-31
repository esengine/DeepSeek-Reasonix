package bytes

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBytesFormat(t *testing.T) {
	// B
	b := Format(0)
	assert.Equal(t, "0", b)
	// B
	b = Format(515)
	assert.Equal(t, "515B", b)

	// KiB
	b = Format(31323)
	assert.Equal(t, "30.59KiB", b)

	// MiB
	b = Format(13231323)
	assert.Equal(t, "12.62MiB", b)

	// GiB
	b = Format(7323232398)
	assert.Equal(t, "6.82GiB", b)

	// TiB
	b = Format(7323232398434)
	assert.Equal(t, "6.66TiB", b)

	// PiB
	b = Format(9923232398434432)
	assert.Equal(t, "8.81PiB", b)

	// EiB
	b = Format(math.MaxInt64)
	assert.Equal(t, "8.00EiB", b)

	// KB
	b = FormatDecimal(31323)
	assert.Equal(t, "31.32KB", b)

	// MB
	b = FormatDecimal(13231323)
	assert.Equal(t, "13.23MB", b)

	// GB
	b = FormatDecimal(7323232398)
	assert.Equal(t, "7.32GB", b)

	// TB
	b = FormatDecimal(7323232398434)
	assert.Equal(t, "7.32TB", b)

	// PB
	b = FormatDecimal(9923232398434432)
	assert.Equal(t, "9.92PB", b)

	// EB
	b = FormatDecimal(math.MaxInt64)
	assert.Equal(t, "9.22EB", b)
}

func TestBytesParseErrors(t *testing.T) {
	_, err := Parse("B999")
	if assert.Error(t, err) {
		assert.EqualError(t, err, "error parsing value=B999")
	}
}

func TestFloats(t *testing.T) {
	// From string:
	str := "12.25KiB"
	value, err := Parse(str)
	assert.NoError(t, err)
	assert.Equal(t, int64(12544), value)

	str2 := Format(value)
	assert.Equal(t, str, str2)

	// To string:
	val := int64(13233029)
	str = Format(val)
	assert.Equal(t, "12.62MiB", str)

	val2, err := Parse(str)
	assert.NoError(t, err)
	assert.Equal(t, val, val2)

	// From string decimal:
	strDec := "12.25KB"
	valueDec, err := Parse(strDec)
	assert.NoError(t, err)
	assert.Equal(t, int64(12250), valueDec)

	strDec2 := FormatDecimal(valueDec)
	assert.Equal(t, strDec, strDec2)

	// To string decimal:
	valDec := int64(13230000)
	strDec = FormatDecimal(valDec)
	assert.Equal(t, "13.23MB", strDec)

	valDec2, err := Parse(strDec)
	assert.NoError(t, err)
	assert.Equal(t, valDec, valDec2)
}

func TestBytesParse(t *testing.T) {
	// B
	b, err := Parse("999")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(999), b)
	}
	b, err = Parse("-100")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(-100), b)
	}
	b, err = Parse("100.1")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(100), b)
	}
	b, err = Parse("515B")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(515), b)
	}

	// B with space
	b, err = Parse("515 B")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(515), b)
	}

	// KiB
	b, err = Parse("12.25KiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12544), b)
	}
	b, err = Parse("12KiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12288), b)
	}
	b, err = Parse("12Ki")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12288), b)
	}

	// kib, lowercase multiple test
	b, err = Parse("12.25kib")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12544), b)
	}
	b, err = Parse("12kib")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12288), b)
	}
	b, err = Parse("12ki")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12288), b)
	}

	// KiB with space
	b, err = Parse("12.25 KiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12544), b)
	}
	b, err = Parse("12 KiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12288), b)
	}
	b, err = Parse("12 Ki")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12288), b)
	}

	// MiB
	b, err = Parse("2MiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(2097152), b)
	}
	b, err = Parse("2Mi")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(2097152), b)
	}

	// GiB with space
	b, err = Parse("6 GiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6442450944), b)
	}
	b, err = Parse("6 Gi")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6442450944), b)
	}

	// GiB
	b, err = Parse("6GiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6442450944), b)
	}
	b, err = Parse("6Gi")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6442450944), b)
	}

	// TiB
	b, err = Parse("5TiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5497558138880), b)
	}
	b, err = Parse("5Ti")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5497558138880), b)
	}

	// TiB with space
	b, err = Parse("5 TiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5497558138880), b)
	}
	b, err = Parse("5 Ti")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5497558138880), b)
	}

	// PiB
	b, err = Parse("9PiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(10133099161583616), b)
	}
	b, err = Parse("9Pi")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(10133099161583616), b)
	}

	// PiB with space
	b, err = Parse("9 PiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(10133099161583616), b)
	}
	b, err = Parse("9 Pi")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(10133099161583616), b)
	}

	// EiB — 7EiB stays within int64; 8EiB == 2^63 overflows and the
	// float-to-int conversion of out-of-range values is
	// implementation-dependent per the Go spec.
	b, err = Parse("7EiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8070450532247928832), b)
	}
	b, err = Parse("7Ei")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8070450532247928832), b)
	}

	// EiB with spaces
	b, err = Parse("7 EiB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8070450532247928832), b)
	}
	b, err = Parse("7 Ei")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8070450532247928832), b)
	}

	// KB
	b, err = Parse("12.25KB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12250), b)
	}
	b, err = Parse("12KB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12000), b)
	}
	b, err = Parse("12K")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12000), b)
	}

	// kb, lowercase multiple test
	b, err = Parse("12.25kb")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12250), b)
	}
	b, err = Parse("12kb")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12000), b)
	}
	b, err = Parse("12k")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12000), b)
	}

	// KB with space
	b, err = Parse("12.25 KB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12250), b)
	}
	b, err = Parse("12 KB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12000), b)
	}
	b, err = Parse("12 K")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(12000), b)
	}

	// MB
	b, err = Parse("2MB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(2000000), b)
	}
	b, err = Parse("2M")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(2000000), b)
	}

	// GB with space
	b, err = Parse("6 GB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6000000000), b)
	}
	b, err = Parse("6 G")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6000000000), b)
	}

	// GB
	b, err = Parse("6GB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6000000000), b)
	}
	b, err = Parse("6G")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(6000000000), b)
	}

	// TB
	b, err = Parse("5TB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5000000000000), b)
	}
	b, err = Parse("5T")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5000000000000), b)
	}

	// TB with space
	b, err = Parse("5 TB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5000000000000), b)
	}
	b, err = Parse("5 T")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(5000000000000), b)
	}

	// PB
	b, err = Parse("9PB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(9000000000000000), b)
	}
	b, err = Parse("9P")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(9000000000000000), b)
	}

	// PB with space
	b, err = Parse("9 PB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(9000000000000000), b)
	}
	b, err = Parse("9 P")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(9000000000000000), b)
	}

	// EB
	b, err = Parse("8EB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8000000000000000000), b)
	}
	b, err = Parse("8E")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8000000000000000000), b)
	}

	// EB with spaces
	b, err = Parse("8 EB")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8000000000000000000), b)
	}
	b, err = Parse("8 E")
	if assert.NoError(t, err) {
		assert.Equal(t, int64(8000000000000000000), b)
	}
}
