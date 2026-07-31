package slicer

import (
	"encoding/json"
	"testing"
)

func TestInt32Add(t *testing.T) {

	s := Int32()
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

func TestInt32AddUnique(t *testing.T) {

	s := Int32()
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

func TestInt32AddSlice(t *testing.T) {

	s := Int32()
	s.Add(1)
	s.Add(2)

	extras := []int32{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt32AddSlicer(t *testing.T) {

	s := Int32()
	s.Add(1)
	s.Add(2)

	p := Int32()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt32Filter(t *testing.T) {

	s := Int32()
	s.Add(18)
	s.Add(180)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i int32) bool {
		return i > 19
	})

	expected := "[180,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt32Each(t *testing.T) {

	s := Int32()
	s.Add(18)
	s.Add(10)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	var result int32

	s.Each(func(i int32) {
		result = result + i
	})

	var expected int32 = 91
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestOptionalInt32Slice tests when you construct a Int32 with
// an existing slice
func TestOptionalInt32Slice(t *testing.T) {
	data := []int32{1, 2, 3}
	s := Int32(data)

	var result int32 = 0
	s.Each(func(elem int32) {
		result += elem
	})
	var expected int32 = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestInt32Sort tests that the slicer can be sorted
func TestInt32Sort(t *testing.T) {
	data := []int32{5, 4, 3, 2, 1}
	s := Int32(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
