package slicer

import (
	"encoding/json"
	"testing"
)

func TestIntAdd(t *testing.T) {

	s := Int()
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

func TestIntAddUnique(t *testing.T) {

	s := Int()
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

func TestIntAddSlice(t *testing.T) {

	s := Int()
	s.Add(1)
	s.Add(2)

	extras := []int{3, 4}

	s.AddSlice(extras)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestIntAddSlicer(t *testing.T) {

	s := Int()
	s.Add(1)
	s.Add(2)

	p := Int()
	p.Add(3)
	p.Add(4)

	s.AddSlicer(p)

	expected := "[1,2,3,4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestIntFilter(t *testing.T) {

	s := Int()
	s.Add(18)
	s.Add(120)
	s.Add(1)
	s.Add(10)
	s.Add(20)
	s.Add(3)
	s.Add(29)

	result := s.Filter(func(i int) bool {
		return i > 19
	})

	expected := "[120,20,29]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

// TestOptionalIntSlice tests when you construct a Int with
// an existing slice
func TestOptionalIntSlice(t *testing.T) {
	data := []int{1, 2, 3}
	s := Int(data)

	var result = 0
	s.Each(func(elem int) {
		result += elem
	})
	var expected = 6
	if expected != result {
		t.Errorf("Expected '%d', but got '%d'", expected, result)
	}
}

// TestIntSort tests that the slicer can be sorted
func TestIntSort(t *testing.T) {
	data := []int{5, 4, 3, 2, 1}
	s := Int(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
