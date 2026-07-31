package slicer

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestBoolAddUnique(t *testing.T) {

	s := Bool()
	s.AddUnique(true)
	s.AddUnique(true)

	expected := "[true]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}

	s.Clear()
	s.AddUnique(true, false)

	expected = "[true,false]"
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestBoolAdd(t *testing.T) {

	s := Bool()
	s.Add(true)
	s.Add(false)

	expected := "[true,false]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}

	s.Clear()
	s.Add(true, false)

	expected = "[true,false]"
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestBoolAddSlice(t *testing.T) {

	s := Bool()
	s.Add(true)
	s.Add(false)

	extras := []bool{true, false}

	s.AddSlice(extras)

	expected := "[true,false,true,false]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestBoolAddSlicer(t *testing.T) {

	s := Bool()
	s.Add(true)
	s.Add(false)

	p := Bool()
	p.Add(true)
	p.Add(false)

	s.AddSlicer(p)

	expected := "[true,false,true,false]"
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestBoolFilter(t *testing.T) {

	s := Bool()
	s.Add(true)
	s.Add(false)
	s.Add(true)
	s.Add(true)
	s.Add(false)

	result := s.Filter(func(i bool) bool {
		return i == true
	})

	expected := 3
	actual := len(result.AsSlice())
	if actual != expected {
		t.Errorf("Expected '%d', but got '%d'", expected, actual)
	}
}

func TestBoolEach(t *testing.T) {

	s := Bool()
	s.Add(true)
	s.Add(false)
	s.Add(true)
	s.Add(true)
	s.Add(false)

	result := ""
	s.Each(func(i bool) {
		result += fmt.Sprintf("%t", i)
	})

	var expected = "truefalsetruetruefalse"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}

// TestOptionalBoolSlice tests
func TestOptionalBoolSlice(t *testing.T) {
	data := []bool{true, false}
	s := Bool(data)

	var result string
	s.Each(func(elem bool) {
		result += fmt.Sprintf("%t", elem)
	})
	var expected = "truefalse"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
