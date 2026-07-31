package tomltest

import (
	"strconv"
	"strings"
	"time"
)

// CompareJSON compares the given arguments.
//
// The returned value is a copy of Test with Failure set to a (human-readable)
// description of the first element that is unequal. If both arguments are
// equal, Test is returned unchanged.
//
// reflect.DeepEqual could work here, but it won't tell us how the two
// structures are different.
func (r Test) CompareJSON(want, have any) Test {
	switch w := want.(type) {
	case map[string]any:
		return r.cmpJSONMaps(w, have)
	case []any:
		return r.cmpJSONArrays(w, have)
	default:
		return r.fail("Key %q in expected output should be a map or a list of maps, but it's a %s", r.Key, fmtType(want))
	}
}

func (r Test) cmpJSONMaps(want map[string]any, have any) Test {
	haveMap, ok := have.(map[string]any)
	if !ok {
		return r.mismatch("table", want, haveMap)
	}

	// Check to make sure both or neither are values.
	if isValue(want) && !isValue(haveMap) {
		return r.fail("Key %q is supposed to be a value, but the parser reports it as a table", r.Key)
	}
	if !isValue(want) && isValue(haveMap) {
		return r.fail("Key %q is supposed to be a table, but the parser reports it as a value", r.Key)
	}
	if isValue(want) && isValue(haveMap) {
		return r.cmpJSONValues(want, haveMap)
	}

	wantKeys, haveKeys := mapKeys(want), mapKeys(haveMap)

	// Check that the keys of each map are equivalent.
	for _, k := range wantKeys {
		if _, ok := haveMap[k]; !ok {
			bunk := r.kjoin(k)
			return bunk.fail("Could not find key %q in parser output.", bunk.Key)
		}
	}
	for _, k := range haveKeys {
		if _, ok := want[k]; !ok {
			bunk := r.kjoin(k)
			return bunk.fail("Could not find key %q in expected output.", bunk.Key)
		}
	}

	// Okay, now make sure that each value is equivalent.
	for _, k := range wantKeys {
		if sub := r.kjoin(k).CompareJSON(want[k], haveMap[k]); sub.Failed() {
			return sub
		}
	}
	return r
}

func (r Test) cmpJSONArrays(want, have any) Test {
	wantSlice, ok := want.([]any)
	if !ok {
		return r.bug("'value' should be a JSON array when 'type=array', but it is a %s", fmtType(want))
	}

	haveSlice, ok := have.([]any)
	if !ok {
		return r.fail(
			"Malformed output from your encoder: 'value' is not a JSON array: %s", fmtType(have))
	}

	if len(wantSlice) != len(haveSlice) {
		return r.fail("Array lengths differ for key %q:\n"+
			"  Expected:     %d\n"+
			"  Your encoder: %d",
			r.Key, len(wantSlice), len(haveSlice))
	}
	for i := 0; i < len(wantSlice); i++ {
		if sub := r.CompareJSON(wantSlice[i], haveSlice[i]); sub.Failed() {
			return sub
		}
	}
	return r
}

func (r Test) cmpJSONValues(want, have map[string]any) Test {
	wantType, ok := want["type"].(string)
	if !ok {
		return r.bug("'type' should be a string, but it is a %s", fmtType(want["type"]))
	}

	haveType, ok := have["type"].(string)
	if !ok {
		return r.fail("Malformed output from your encoder: 'type' is not a string: %s", fmtType(have["type"]))
	}

	if wantType == "integer" && r.IntAsFloat {
		wantType = "float"
	}

	if wantType != haveType {
		return r.valMismatch(wantType, haveType, want, have)
	}

	// If this is an array, then we've got to do some work to check equality.
	if wantType == "array" {
		return r.cmpJSONArrays(want, have)
	}

	// Atomic values are always strings
	wantVal, ok := want["value"].(string)
	if !ok {
		return r.bug("'value' %v should be a string, but it is a %s", want["value"], fmtType(want["value"]))
	}

	haveVal, ok := have["value"].(string)
	if !ok {
		return r.fail("Malformed output from your encoder: %s is not a string", fmtType(have["value"]))
	}

	// Excepting floats and datetimes, other values can be compared as strings.
	switch wantType {
	case "float":
		return r.cmpFloats(wantVal, haveVal)
	case "datetime", "datetime-local", "date-local", "time-local":
		return r.cmpAsDatetimes(wantType, wantVal, haveVal)
	default:
		if wantType == "bool" {
			wantVal, haveVal = strings.ToLower(wantVal), strings.ToLower(haveVal)
		}
		return r.cmpAsStrings(wantVal, haveVal)
	}
}

func (r Test) cmpAsStrings(want, have string) Test {
	if want != have {
		return r.fail("Values for key %q don't match:\n"+
			"  Expected:     %s\n"+
			"  Your encoder: %s",
			r.Key, want, have)
	}
	return r
}

func (r Test) cmpFloats(want, have string) Test {
	// Special case for NaN, since NaN != NaN.
	want, have = strings.ToLower(want), strings.ToLower(have)
	if strings.HasSuffix(want, "nan") || strings.HasSuffix(have, "nan") {
		want, have := strings.TrimLeft(want, "-+"), strings.TrimLeft(have, "-+")
		if want != have {
			return r.fail("Values for key %q don't match:\n"+
				"  Expected:     %v\n"+
				"  Your encoder: %v",
				r.Key, want, have)
		}
		return r
	}

	wantF, err := strconv.ParseFloat(want, 64)
	if err != nil {
		return r.bug("Could not read %q as a float value for key %q", want, r.Key)
	}

	haveF, err := strconv.ParseFloat(have, 64)
	if err != nil {
		return r.fail("Malformed output from your encoder: key %q is not a float: %q", r.Key, have)
	}

	if wantF != haveF {
		return r.fail("Values for key %q don't match:\n"+
			"  Expected:     %v\n"+
			"  Your encoder: %v",
			r.Key, wantF, haveF)
	}
	return r
}

var datetimeRepl = strings.NewReplacer(
	" ", "T",
	"t", "T",
	"z", "Z")

var layouts = map[string]string{
	"datetime":       time.RFC3339Nano,
	"datetime-local": "2006-01-02T15:04:05.999999999",
	"date-local":     "2006-01-02",
	"time-local":     "15:04:05",
}

func (r Test) cmpAsDatetimes(kind, want, have string) Test {
	layout, ok := layouts[kind]
	if !ok {
		panic("should never happen")
	}

	wantT, err := time.Parse(layout, datetimeRepl.Replace(want))
	if err != nil {
		return r.bug("Could not read %q as a datetime value for key %q", want, r.Key)
	}

	haveT, err := time.Parse(layout, datetimeRepl.Replace(want))
	if err != nil {
		return r.fail("Malformed output from your encoder: key %q is not a datetime: %q", r.Key, have)
	}
	if !wantT.Equal(haveT) {
		return r.fail("Values for key %q don't match:\n"+
			"  Expected:     %v\n"+
			"  Your encoder: %v",
			r.Key, wantT, haveT)
	}
	return r
}

func (r Test) kjoin(key string) Test {
	if len(r.Key) == 0 {
		r.Key = key
	} else {
		r.Key += "." + key
	}
	return r
}

func isValue(m map[string]any) bool {
	if len(m) != 2 {
		return false
	}
	if _, ok := m["type"]; !ok {
		return false
	}
	if _, ok := m["value"]; !ok {
		return false
	}
	return true
}

func (r Test) mismatch(wantType string, want, have any) Test {
	return r.fail("Key %[1]q (type %[2]q):\n"+
		"  Expected:     %s\n"+
		"  Your encoder: %s",
		r.Key, wantType, fmtHashV(have), fmtType(have))
}

func (r Test) valMismatch(wantType, haveType string, want, have any) Test {
	return r.fail("Key %q is not %q but %q:\n"+
		"  Expected:     %s\n"+
		"  Your encoder: %s",
		r.Key, wantType, haveType, fmtHashV(want), fmtHashV(have))
}
