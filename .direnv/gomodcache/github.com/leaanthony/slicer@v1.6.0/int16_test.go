package slicer

import (
	"encoding/json"
	"testing"
)

func TestInt16Add(t *testing.T) {

	s := Int16()
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

func TestInt16AddUnique(t *testing.T) {

	s := Int16()
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

func TestInt16AddSlice(t *testing.T) {

	s := Int16()
	s.Add(1)
	s.Add(2)

	extras := []int16{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt16AddSlicer(t *testing.T) {

	s := Int16()
	s.Add(1)
	s.Add(2)

	p := Int16()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestInt16Filter(t *testing.T) {

	s := Int16()
	s.Add(18)
	s.Add(180)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i int16) bool {
		return i > 19
	})

	expected := "[180,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestInt16Each(t *testing.T) {

	s := Int16()
	s.Add(18)
	s.Add(10)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	var result int16

	s.Each(func(i int16) {
		result = result + i
	})

	var expected int16 = 91
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestOptionalInt16Slice tests when you construct a Int16 with
// an existing slice
func TestOptionalInt16Slice(t *testing.T) {
	data := []int16{1, 2, 3}
	s := Int16(data)

	var result int16 = 0
	s.Each(func(elem int16) {
		result += elem
	})
	var expected int16 = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestInt16Sort tests that the slicer can be sorted
func TestInt16Sort(t *testing.T) {
	data := []int16{5, 4, 3, 2, 1}
	s := Int16(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
