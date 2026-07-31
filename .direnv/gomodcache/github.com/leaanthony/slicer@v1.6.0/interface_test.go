package slicer

import (
	"encoding/json"
	"testing"
)

func TestInterfaceAdd(t *testing.T) {

	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a)
	s.Add(b)

	expected := `[1,"hello"]`
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}

	// Add more than one value
	s.Clear()
	s.Add(a, b)
	expected = `[1,"hello"]`
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInterfaceAddUnique(t *testing.T) {

	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.AddUnique(a)
	s.AddUnique(a)
	s.AddUnique(b)

	expected := `[1,"hello"]`
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}

	// AddUnique more than one value
	s.Clear()
	s.AddUnique(a, b, b, a)
	expected = `[1,"hello"]`
	actual, _ = json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInterfaceAddSlice(t *testing.T) {

	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a)
	s.Add(b)

	extras := []interface{}{true, 6.6}

	s.AddSlice(extras)

	expected := `[1,"hello",true,6.6]`
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}
func TestInterfaceAddSlicer(t *testing.T) {

	s := Interface()
	var a interface{} = 1
	var b interface{} = "hello"
	s.Add(a)
	s.Add(b)

	p := Interface()
	var c interface{} = true
	var d interface{} = 6.6
	p.Add(c)
	p.Add(d)

	s.AddSlicer(p)

	expected := `[1,"hello",true,6.6]`
	actual, _ := json.Marshal(s.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInterfaceFilter(t *testing.T) {

	s := Interface()
	s.Add(1)
	s.Add("hello")
	s.Add(true)
	s.Add("world")
	s.Add(9.9)

	result := s.Filter(func(i interface{}) bool {
		_, ok := i.(string)
		return ok
	})

	expected := `["hello","world"]`
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

func TestInterfaceEach(t *testing.T) {

	s := Interface()
	s.Add(1)
	s.Add("hello")
	s.Add(true)
	s.Add("world")
	s.Add(9.9)

	result := String()
	s.Each(func(i interface{}) {
		val, ok := i.(string)
		if ok {
			result.Add(val + "!")
		}
	})

	expected := `["hello!","world!"]`
	actual, _ := json.Marshal(result.AsSlice())
	if expected != string(actual) {
		t.Errorf("Expected '%s', but got '%s'", expected, actual)
	}
}

// TestOptionalInterfaceSlice tests when you construct a Interface with
// an existing slice
func TestOptionalInterfaceSlice(t *testing.T) {
	data := []interface{}{"one", "two", "three"}
	s := Interface(data)

	result := ""
	s.Each(func(elem interface{}) {
		result += elem.(string)
	})
	expected := "onetwothree"
	if expected != result {
		t.Errorf("Expected '%s', but got '%s'", expected, result)
	}
}
