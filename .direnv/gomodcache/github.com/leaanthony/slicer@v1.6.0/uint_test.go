package slicer

import (
	"encoding/json"
	"github.com/matryer/is"
	"testing"
)

func TestUintAdd(t *testing.T) {

	s := Uint()
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
func TestUintDeduplicate(t *testing.T) {
	is2 := is.New(t)
	s := Uint()
	s.Add(1)
	s.Add(2)
	s.Add(2)
	s.Add(2)

	is2.Equal(s.Length(), 4)
	is2.Equal(s.AsSlice(), []uint{1, 2, 2, 2})
	s.Deduplicate()
	is2.Equal(s.Length(), 2)
	is2.Equal(s.AsSlice(), []uint{1, 2})
}

func TestUintLength(t *testing.T) {
	is2 := is.New(t)
	s := Uint()
	s.Add(1)
	s.Add(2)

	is2.Equal(s.Length(), 2)
}

func TestUintAddUnique(t *testing.T) {

	s := Uint()
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

func TestUintAddSlice(t *testing.T) {

	s := Uint()
	s.Add(1)
	s.Add(2)

	extras := []uint{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestUintAddSlicer(t *testing.T) {

	s := Uint()
	s.Add(1)
	s.Add(2)

	p := Uint()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestUintFilter(t *testing.T) {

	s := Uint()
	s.Add(18)
	s.Add(120)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i uint) bool {
		return i > 19
	})

	expected := "[120,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

// TestOptionalUintSlice tests when you construct a Uint with
// an existing slice
func TestOptionalUintSlice(t *testing.T) {
	data := []uint{1, 2, 3}
	s := Uint(data)

	var result uint = 0
	s.Each(func(elem uint) {
		result += elem
	})
	var expected uint = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestUintSort tests that the slicer can be sorted
func TestUintSort(t *testing.T) {
	is2 := is.New(t)
	data := []uint{5, 4, 3, 2, 1}
	s := Uint(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	is2.Equal(expected, result)
	s.Clear()
	is2.Equal(s.Join(""), "")
}
