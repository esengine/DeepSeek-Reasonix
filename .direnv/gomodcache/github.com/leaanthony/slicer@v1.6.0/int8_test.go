package slicer

import (
	"encoding/json"
	"testing"
)

func TestInt8Add(t *testing.T) {

	s := Int8()
	s.Add(1)
	s.Add(2)

	expected := "[1,2]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
	s.Clear()
	s.Add(1, 2)

	expected = "[1,2]"
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestInt8AddUnique(t *testing.T) {

	s := Int8()
	s.AddUnique(1)
	s.AddUnique(2)
	s.AddUnique(2)
	s.AddUnique(2)

	expected := "[1,2]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}

	s.Clear()
	s.AddUnique(1, 2, 1, 2, 2, 1)

	expected = "[1,2]"
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestInt8AddSlice(t *testing.T) {

	s := Int8()
	s.Add(1)
	s.Add(2)

	extras := []int8{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt8AddSlicer(t *testing.T) {

	s := Int8()
	s.Add(1)
	s.Add(2)

	p := Int8()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestInt8Filter(t *testing.T) {

	s := Int8()
	s.Add(18)
	s.Add(120)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i int8) bool {
		return i > 19
	})

	expected := "[120,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt8Each(t *testing.T) {

	s := Int8()
	s.Add(18)
	s.Add(10)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	var result int8

	s.Each(func(i int8) {
		result = result + i
	})

	var expected int8 = 91
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestOptionalInt8Slice tests when you construct a Int8 with
// an existing slice
func TestOptionalInt8Slice(t *testing.T) {
	data := []int8{1, 2, 3}
	s := Int8(data)

	var result int8 = 0
	s.Each(func(elem int8) {
		result += elem
	})
	var expected int8 = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestInt8Sort tests that the slicer can be sorted
func TestInt8Sort(t *testing.T) {
	data := []int8{5, 4, 3, 2, 1}
	s := Int8(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
