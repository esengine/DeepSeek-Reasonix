package slicer

import (
	"encoding/json"
	"github.com/matryer/is"
	"testing"
)

func TestUint16Add(t *testing.T) {

	s := Uint16()
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

func TestUint16Length(t *testing.T) {
	is2 := is.New(t)
	s := Uint16()
	s.Add(1)
	s.Add(2)

	is2.Equal(s.Length(), 2)
}
func TestUint16Deduplicate(t *testing.T) {
	is2 := is.New(t)
	s := Uint16()
	s.Add(1)
	s.Add(2)
	s.Add(2)
	s.Add(2)

	is2.Equal(s.Length(), 4)
	is2.Equal(s.AsSlice(), []uint16{1, 2, 2, 2})
	s.Deduplicate()
	is2.Equal(s.Length(), 2)
	is2.Equal(s.AsSlice(), []uint16{1, 2})
}
func TestUint16AddUnique(t *testing.T) {

	s := Uint16()
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

func TestUint16AddSlice(t *testing.T) {

	s := Uint16()
	s.Add(1)
	s.Add(2)

	extras := []uint16{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestUint16AddSlicer(t *testing.T) {

	s := Uint16()
	s.Add(1)
	s.Add(2)

	p := Uint16()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestUint16Filter(t *testing.T) {

	s := Uint16()
	s.Add(18)
	s.Add(180)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i uint16) bool {
		return i > 19
	})

	expected := "[180,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestUint16Each(t *testing.T) {

	s := Uint16()
	s.Add(18)
	s.Add(10)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	var result uint16

	s.Each(func(i uint16) {
		result = result + i
	})

	var expected uint16 = 91
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestOptionalUint16Slice tests when you construct a Uint16 with
// an existing slice
func TestOptionalUint16Slice(t *testing.T) {
	data := []uint16{1, 2, 3}
	s := Uint16(data)

	var result uint16 = 0
	s.Each(func(elem uint16) {
		result += elem
	})
	var expected uint16 = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestUint16Sort tests that the slicer can be sorted
func TestUint16Sort(t *testing.T) {
	is2 := is.New(t)
	data := []uint16{5, 4, 3, 2, 1}
	s := Uint16(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	is2.Equal(result, expected)
	s.Clear()
	is2.Equal(s.Join(""), "")
}
