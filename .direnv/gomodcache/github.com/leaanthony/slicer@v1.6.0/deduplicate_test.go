package slicer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStringDeduplicate(t *testing.T) {

	s := String()
	s.Add("one", "four", "two", "three", "two", "one")

	s.Deduplicate()
	expected := "one four two three"
	actual := strings.Join(s.AsSlice(), " ")
	if expected != actual {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt64Deduplicate(t *testing.T) {

	s := Int64()
	s.Add(1, 2, 3, 2, 3, 4, 3, 4, 5)

	s.Deduplicate()
	expected := "[1,2,3,4,5]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt32Deduplicate(t *testing.T) {

	s := Int32()
	s.Add(1, 2, 3, 2, 3, 4, 3, 4, 5)

	s.Deduplicate()
	expected := "[1,2,3,4,5]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt16Deduplicate(t *testing.T) {

	s := Int16()
	s.Add(1, 2, 3, 2, 3, 4, 3, 4, 5)

	s.Deduplicate()
	expected := "[1,2,3,4,5]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt8Deduplicate(t *testing.T) {

	s := Int8()
	s.Add(1, 2, 3, 2, 3, 4, 3, 4, 5)

	s.Deduplicate()
	expected := "[1,2,3,4,5]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestIntDeduplicate(t *testing.T) {

	s := Int()
	s.Add(1, 2, 3, 2, 3, 4, 3, 4, 5)

	s.Deduplicate()
	expected := "[1,2,3,4,5]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInterfaceDeduplicate(t *testing.T) {

	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a, b, a, b)

	s.Deduplicate()
	expected := `[1,"hello"]`
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFloat32Deduplicate(t *testing.T) {

	s := Float32()
	s.Add(1.5, 2.3, 3.9, 2.3, 3.9, 4.4, 3.9, 4.4, 5.2)

	s.Deduplicate()
	expected := "[1.5,2.3,3.9,4.4,5.2]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFloat64Deduplicate(t *testing.T) {

	s := Float64()
	s.Add(1.5, 2.3, 3.9, 2.3, 3.9, 4.4, 3.9, 4.4, 5.2)

	s.Deduplicate()
	expected := "[1.5,2.3,3.9,4.4,5.2]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestBoolDeduplicate(t *testing.T) {

	s := Bool()
	s.Add(true, true, true, false, true, false, false, false)

	s.Deduplicate()
	expected := "[true,false]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

// func TestStringAddSlice(t *testing.T) {

// 	s := String()
// 	s.Add("one")
// 	s.Add("two")
// 	extras := []string{"three", "four"}
// 	s.AddSlice(extras)

// 	expected := "one two three four"
// 	actual := strings.Join(s.AsSlice(), " ")
// 	if expected != actual {
// 		t.Errorf("Expected '%s', but got '%s'", expected, actual)
// 	}
// }

// func TestStringAddSlicer(t *testing.T) {

// 	s := String()
// 	s.Add("one")
// 	s.Add("two")

// 	p := String()
// 	p.Add("three")
// 	p.Add("four")

// 	s.AddSlicer(p)

// 	expected := "one two three four"
// 	actual := strings.Join(s.AsSlice(), " ")
// 	if expected != actual {
// 		t.Errorf("Expected '%s', but got '%s'", expected, actual)
// 	}
// }
