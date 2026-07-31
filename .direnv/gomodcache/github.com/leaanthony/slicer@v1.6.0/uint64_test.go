package slicer

import (
	"encoding/json"
	"github.com/matryer/is"
	"testing"
)

func TestUint64Add(t *testing.T) {

	s := Uint64()
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
func TestUint64AddUnique(t *testing.T) {

	s := Uint64()
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

func TestUint64AddSlice(t *testing.T) {

	s := Uint64()
	s.Add(1)
	s.Add(2)

	extras := []uint64{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestUint64AddSlicer(t *testing.T) {

	s := Uint64()
	s.Add(1)
	s.Add(2)

	p := Uint64()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestUint64Filter(t *testing.T) {

	s := Uint64()
	s.Add(18)
	s.Add(180)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i uint64) bool {
		return i > 19
	})

	expected := "[180,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestUint64Each(t *testing.T) {

	s := Uint64()
	s.Add(18)
	s.Add(10)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	var result uint64

	s.Each(func(i uint64) {
		result = result + i
	})

	var expected uint64 = 91
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestOptionalUint64Slice tests when you construct a Uint64 with
// an existing slice
func TestOptionalUint64Slice(t *testing.T) {
	data := []uint64{1, 2, 3}
	s := Uint64(data)

	var result uint64 = 0
	s.Each(func(elem uint64) {
		result += elem
	})
	var expected uint64 = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}
func TestUint64Deduplicate(t *testing.T) {
	is2 := is.New(t)
	s := Uint64()
	s.Add(1)
	s.Add(2)
	s.Add(2)
	s.Add(2)

	is2.Equal(s.Length(), 4)
	is2.Equal(s.AsSlice(), []uint64{1, 2, 2, 2})
	s.Deduplicate()
	is2.Equal(s.Length(), 2)
	is2.Equal(s.AsSlice(), []uint64{1, 2})
}
func TestUint64Length(t *testing.T) {
	is2 := is.New(t)
	s := Uint64()
	s.Add(1)
	s.Add(2)

	is2.Equal(s.Length(), 2)
}

// TestUint64Sort tests that the slicer can be sorted
func TestUint64Sort(t *testing.T) {
	is2 := is.New(t)
	data := []uint64{5, 4, 3, 2, 1}
	s := Uint64(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	is2.Equal(expected, result)
	s.Clear()
	is2.Equal(s.Join(""), "")
}
