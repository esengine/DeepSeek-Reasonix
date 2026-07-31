package slicer

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestFloat64Add(t *testing.T) {

	s := Float64()
	s.Add(1.1)
	s.Add(2.5)

	expected := "[1.1,2.5]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
	s.Clear()
	s.Add(1.1, 2.5)

	expected = "[1.1,2.5]"
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestFloat64AddUnique(t *testing.T) {

	s := Float64()
	s.AddUnique(1.1)
	s.AddUnique(2.5)
	s.AddUnique(2.5)

	expected := "[1.1,2.5]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
	s.Clear()
	s.AddUnique(1.1, 2.5, 1.1)

	expected = "[1.1,2.5]"
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFloat64AddSlice(t *testing.T) {

	s := Float64()
	s.Add(1.1)
	s.Add(2.2)

	extras := []float64{3.3, 4.4}

	s.AddSlice(extras)

	expected := "[1.1,2.2,3.3,4.4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFloat64AddSlicer(t *testing.T) {

	s := Float64()
	s.Add(1.1)
	s.Add(2.2)

	p := Float64()
	p.Add(3.3)
	p.Add(4.4)

	s.AddSlicer(p)

	expected := "[1.1,2.2,3.3,4.4]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFloat64Filter(t *testing.T) {

	s := Float64()
	s.Add(0.1)
	s.Add(0.4)
	s.Add(0.3)
	s.Add(0.8)
	s.Add(0.5)
	s.Add(1.3)
	s.Add(3.9)
	s.Add(0.4)

	result := s.Filter(func(i float64) bool {
		return i < 0.5
	})

	expected := "[0.1,0.4,0.3,0.4]"
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestFloat64Each(t *testing.T) {

	s := Float64()
	s.Add(0.1)
	s.Add(0.4)
	s.Add(0.3)
	s.Add(0.8)
	s.Add(0.5)
	s.Add(1.3)
	s.Add(3.9)
	s.Add(0.4)

	var result float64
	s.Each(func(i float64) {
		result += i
	})

	var expected = 7.7
	if fmt.Sprintf("%f", expected) != fmt.Sprintf("%f", result) {
		t.Errorf("Expected '%f', but got '%f'", expected, result)
	}
}

// TestOptionalFloat64Slice tests when you construct a Float64 with
// an existing slice
func TestOptionalFloat64Slice(t *testing.T) {
	data := []float64{1, 2, 3}
	s := Float64(data)

	var result float64
	s.Each(func(elem float64) {
		result += elem
	})
	var expected = 6.0
	if expected != result {
		t.Errorf("Expected '%f', but got '%f'", expected, result)
	}
}

// TestFloat64Sort tests that the slicer can be sorted
func TestFloat64Sort(t *testing.T) {
	data := []float64{5, 4, 3, 2, 1}
	s := Float64(data)
	s.Sort()
	result := s.Join(",")
	expected := "1,2,3,4,5"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
