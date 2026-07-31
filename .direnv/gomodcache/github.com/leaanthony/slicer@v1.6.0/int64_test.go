package slicer

import (
	"encoding/json"
	"testing"
)

func TestInt64Add(t *testing.T) {

	s := Int64()
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
func TestInt64AddUnique(t *testing.T) {

	s := Int64()
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

func TestInt64AddSlice(t *testing.T) {

	s := Int64()
	s.Add(1)
	s.Add(2)

	extras := []int64{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt64AddSlicer(t *testing.T) {

	s := Int64()
	s.Add(1)
	s.Add(2)

	p := Int64()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt64Filter(t *testing.T) {

	s := Int64()
	s.Add(18)
	s.Add(180)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i int64) bool {
		return i > 19
	})

	expected := "[180,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInt64Each(t *testing.T) {

	s := Int64()
	s.Add(18)
	s.Add(10)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	var result int64

	s.Each(func(i int64) {
		result = result + i
	})

	var expected int64 = 91
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestOptionalInt64Slice tests when you construct a Int64 with
// an existing slice
func TestOptionalInt64Slice(t *testing.T) {
	data := []int64{1, 2, 3}
	s := Int64(data)

	var result int64 = 0
	s.Each(func(elem int64) {
		result += elem
	})
	var expected int64 = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestInt64Sort tests that the slicer can be sorted
func TestInt64Sort(t *testing.T) {
	data := []int64{5, 4, 3, 2, 1}
	s := Int64(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
